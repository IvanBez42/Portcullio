package socket

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/dockerctl"
	"github.com/IvanBez42/Portcullio/agent/internal/mount"
	"github.com/IvanBez42/Portcullio/agent/internal/provision"
	"github.com/IvanBez42/Portcullio/agent/internal/vault"
)

// Validated-identifier gate for every vault_id //
var vaultIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_-]{0,63}$`)

func validVaultID(id string) bool {
	return vaultIDPattern.MatchString(id)
}

// Returns an AgentHandler for cfg //
func NewAgentHandler(cfg HandlerConfig) *AgentHandler {
	return &AgentHandler{cfg: cfg, vaults: make(map[string]*vault.Vault)}
}

func (h *AgentHandler) imagePath(id string) string {
	return filepath.Join(h.cfg.InputDir, id+".img")
}

func (h *AgentHandler) mountPath(id string) string {
	return filepath.Join(h.cfg.MountAreaDir, id)
}

func (h *AgentHandler) mapperName(id string) string {
	return "portcullio-" + id
}

func (h *AgentHandler) vaultExists(id string) (bool, error) {
	_, err := os.Stat(h.imagePath(id))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("socket: check vault %q exists: %w", id, err)
}

// Lists every vault_id found in InputDir //
func (h *AgentHandler) listVaultIDs() ([]string, error) {
	entries, err := os.ReadDir(h.cfg.InputDir)
	if err != nil {
		return nil, fmt.Errorf("socket: list vaults in %s: %w", h.cfg.InputDir, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext == ".img" {
			ids = append(ids, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Returns the registered Vault for id, creating one on first reference //
func (h *AgentHandler) getOrCreateVault(id string) (*vault.Vault, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if v, ok := h.vaults[id]; ok {
		return v, nil
	}

	mountPath := h.mountPath(id)
	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		return nil, fmt.Errorf("socket: create mount dir for vault %q: %w", id, err)
	}

	v := vault.New(vault.Config{
		ImagePath:  h.imagePath(id),
		MapperName: h.mapperName(id),
		Fstype:     h.cfg.Fstype,
		MountPath:  mountPath,
	})
	if err := v.EnsureSealed(); err != nil {
		return nil, fmt.Errorf("socket: ensure vault %q sealed: %w", id, err)
	}

	h.vaults[id] = v
	return v, nil
}

func (h *AgentHandler) forgetVault(id string) {
	h.mu.Lock()
	delete(h.vaults, id)
	h.mu.Unlock()
}

// Startup convergence step for every known vault //
func (h *AgentHandler) ReconcileAll(handleTimeout, pollInterval time.Duration, log func(string)) error {
	ids, err := h.listVaultIDs()
	if err != nil {
		return fmt.Errorf("socket: reconcile all: %w", err)
	}
	for _, id := range ids {
		v, err := h.getOrCreateVault(id)
		if err != nil {
			log(fmt.Sprintf("vault %q: failed to initialize for startup reconciliation: %v", id, err))
			continue
		}
		healed, info, err := v.Reconcile(handleTimeout, pollInterval)
		if err != nil {
			log(fmt.Sprintf("vault %q: needs attention after startup reconciliation: %v (state: %s)", id, err, info.State))
			continue
		}
		if healed {
			log(fmt.Sprintf("vault %q: auto-healed from degraded state at startup", id))
		}
	}
	return nil
}

// Dispatches req to the handler for its verb //
func (h *AgentHandler) Handle(req Request) Response {
	switch req.Verb {
	case VerbStatus:
		return h.handleStatus(req)
	case VerbUnseal:
		return h.handleUnseal(req)
	case VerbSeal:
		return h.handleSeal(req)
	case VerbCreate:
		return h.handleCreate(req)
	case VerbDestroy:
		return h.handleDestroy(req)
	case VerbServices:
		return h.handleServices(req)
	case VerbSpace:
		return h.handleSpace(req)
	default:
		return errResp(fmt.Errorf("socket: unknown verb %q", req.Verb))
	}
}

func errResp(err error) Response {
	return Response{OK: false, Error: err.Error()}
}

func (h *AgentHandler) statusOne(id string) (VaultStatus, error) {
	v, err := h.getOrCreateVault(id)
	if err != nil {
		return VaultStatus{}, err
	}
	info, err := v.Status()
	if err != nil {
		return VaultStatus{}, fmt.Errorf("socket: status %q: %w", id, err)
	}

	status := VaultStatus{VaultID: id, State: info.State.String(), Detail: info.Detail}

	if fi, err := os.Stat(h.imagePath(id)); err == nil {
		status.TotalMB = fi.Size() / (1024 * 1024)
	}
	if info.State == vault.Unsealed {
		if used, err := mount.DiskUsage(h.mountPath(id)); err == nil {
			status.UsedMB = used / (1024 * 1024)
		}
	}

	return status, nil
}

// Handles the status verb //
func (h *AgentHandler) handleStatus(req Request) Response {
	if req.VaultID == "" {
		ids, err := h.listVaultIDs()
		if err != nil {
			return errResp(err)
		}
		vaults := make([]VaultStatus, 0, len(ids))
		for _, id := range ids {
			info, err := h.statusOne(id)
			if err != nil {
				return errResp(err)
			}
			vaults = append(vaults, info)
		}
		return Response{OK: true, Vaults: vaults}
	}

	if !validVaultID(req.VaultID) {
		return errResp(fmt.Errorf("socket: invalid vault_id %q", req.VaultID))
	}
	exists, err := h.vaultExists(req.VaultID)
	if err != nil {
		return errResp(err)
	}
	if !exists {
		return errResp(fmt.Errorf("socket: vault %q not found", req.VaultID))
	}
	info, err := h.statusOne(req.VaultID)
	if err != nil {
		return errResp(err)
	}
	return Response{OK: true, Vaults: []VaultStatus{info}}
}

// Handles the unseal verb: stop services -> Unseal -> start services //
func (h *AgentHandler) handleUnseal(req Request) Response {
	if !validVaultID(req.VaultID) {
		zeroBytes(req.Passphrase)
		return errResp(fmt.Errorf("socket: invalid vault_id %q", req.VaultID))
	}
	exists, err := h.vaultExists(req.VaultID)
	if err != nil {
		zeroBytes(req.Passphrase)
		return errResp(err)
	}
	if !exists {
		zeroBytes(req.Passphrase)
		return errResp(fmt.Errorf("socket: vault %q not found", req.VaultID))
	}

	v, err := h.getOrCreateVault(req.VaultID)
	if err != nil {
		zeroBytes(req.Passphrase)
		return errResp(err)
	}

	var stopErr error
	if len(req.Services) > 0 {
		stopErr = dockerctl.Stop(req.Services)
	}

	// v.Unseal zeroes req.Passphrase //
	if err := v.Unseal(req.Passphrase); err != nil {
		return errResp(fmt.Errorf("socket: unseal %q: %w", req.VaultID, err))
	}

	if len(req.Services) > 0 {
		if err := dockerctl.Start(req.Services); err != nil {
			return Response{OK: false, State: vault.Unsealed.String(),
				Error: fmt.Sprintf("vault unsealed but starting services failed: %v", err)}
		}
	}
	if stopErr != nil {
		return Response{OK: true, State: vault.Unsealed.String(),
			Error: fmt.Sprintf("vault unsealed and services started, but stopping them first failed: %v", stopErr)}
	}
	return Response{OK: true, State: vault.Unsealed.String()}
}

// Handles the seal verb: stop services -> Seal //
func (h *AgentHandler) handleSeal(req Request) Response {
	if !validVaultID(req.VaultID) {
		return errResp(fmt.Errorf("socket: invalid vault_id %q", req.VaultID))
	}
	exists, err := h.vaultExists(req.VaultID)
	if err != nil {
		return errResp(err)
	}
	if !exists {
		return errResp(fmt.Errorf("socket: vault %q not found", req.VaultID))
	}

	v, err := h.getOrCreateVault(req.VaultID)
	if err != nil {
		return errResp(err)
	}

	var stopErr error
	if len(req.Services) > 0 {
		stopErr = dockerctl.Stop(req.Services)
	}

	sealErr := v.Seal(h.cfg.SealHandleTimeout, h.cfg.SealPollInterval)
	if sealErr != nil {
		if stopErr != nil {
			return errResp(fmt.Errorf("socket: seal %q: %w (also, stopping services failed: %v)", req.VaultID, sealErr, stopErr))
		}
		return errResp(fmt.Errorf("socket: seal %q: %w", req.VaultID, sealErr))
	}
	if stopErr != nil {
		return Response{OK: true, State: vault.Sealed.String(),
			Error: fmt.Sprintf("vault sealed, but stopping services failed: %v", stopErr)}
	}
	return Response{OK: true, State: vault.Sealed.String()}
}

// Handles the create verb //
func (h *AgentHandler) handleCreate(req Request) Response {
	if !validVaultID(req.VaultID) {
		zeroBytes(req.Passphrase)
		return errResp(fmt.Errorf("socket: invalid vault_id %q", req.VaultID))
	}
	exists, err := h.vaultExists(req.VaultID)
	if err != nil {
		zeroBytes(req.Passphrase)
		return errResp(err)
	}
	if exists {
		zeroBytes(req.Passphrase)
		return errResp(fmt.Errorf("socket: vault %q already exists", req.VaultID))
	}

	if err := os.MkdirAll(h.cfg.InputDir, 0o700); err != nil {
		zeroBytes(req.Passphrase)
		return errResp(fmt.Errorf("socket: create input dir: %w", err))
	}

	// CreateVault zeroes req.Passphrase //
	err = provision.CreateVault(provision.CreateVaultParams{
		ImagePath:  h.imagePath(req.VaultID),
		SizeMB:     req.SizeMB,
		Fstype:     h.cfg.Fstype,
		MapperName: h.mapperName(req.VaultID),
		Passphrase: req.Passphrase,
	})
	if err != nil {
		return errResp(fmt.Errorf("socket: create %q: %w", req.VaultID, err))
	}

	if _, err := h.getOrCreateVault(req.VaultID); err != nil {
		return errResp(fmt.Errorf("socket: created %q but failed to initialize it: %w", req.VaultID, err))
	}
	return Response{OK: true, State: vault.Sealed.String()}
}

// Handles the destroy verb //
func (h *AgentHandler) handleDestroy(req Request) Response {
	if !validVaultID(req.VaultID) {
		return errResp(fmt.Errorf("socket: invalid vault_id %q", req.VaultID))
	}
	exists, err := h.vaultExists(req.VaultID)
	if err != nil {
		return errResp(err)
	}
	if !exists {
		return errResp(fmt.Errorf("socket: vault %q not found", req.VaultID))
	}

	if err := provision.DestroyVault(h.imagePath(req.VaultID), h.mapperName(req.VaultID), h.mountPath(req.VaultID)); err != nil {
		return errResp(fmt.Errorf("socket: destroy %q: %w", req.VaultID, err))
	}
	h.forgetVault(req.VaultID)
	return Response{OK: true}
}

// Handles the services verb //
func (h *AgentHandler) handleServices(req Request) Response {
	names, err := dockerctl.LinkableContainers()
	if err != nil {
		return errResp(fmt.Errorf("socket: list services: %w", err))
	}
	return Response{OK: true, Services: names}
}

// Handles the space verb //
func (h *AgentHandler) handleSpace(req Request) Response {
	avail, err := provision.AvailableSpace(h.cfg.InputDir)
	if err != nil {
		return errResp(fmt.Errorf("socket: available space: %w", err))
	}
	return Response{OK: true, AvailableMB: avail / (1024 * 1024)}
}
