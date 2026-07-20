package socket

import (
	"net"
	"sync"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/vault"
)

// Wire shape for every request //
type Request struct {
	Verb       string   `json:"verb"`
	VaultID    string   `json:"vault_id,omitempty"`
	Passphrase []byte   `json:"passphrase,omitempty"`
	Services   []string `json:"services,omitempty"`
	SizeMB     int      `json:"size_mb,omitempty"`
}

// One vault's entry in a status Response //
type VaultStatus struct {
	VaultID string `json:"vault_id"`
	State   string `json:"state"`
	Detail  string `json:"detail,omitempty"`
	TotalMB int64  `json:"total_mb,omitempty"`
	UsedMB  int64  `json:"used_mb,omitempty"`
}

// Wire shape for every response //
type Response struct {
	OK          bool          `json:"ok"`
	Error       string        `json:"error,omitempty"`
	State       string        `json:"state,omitempty"`
	Vaults      []VaultStatus `json:"vaults,omitempty"`
	Services    []string      `json:"services,omitempty"`
	AvailableMB int64         `json:"available_mb,omitempty"`
}

// Listens on a Unix socket, dispatches to an AgentHandler //
type Server struct {
	ln      net.Listener
	handler *AgentHandler
}

// AgentHandler's static configuration //
type HandlerConfig struct {
	InputDir     string // holds <vault_id>.img backing files
	MountAreaDir string // parent of per-vault mount paths, <MountAreaDir>/<vault_id>

	Fstype string

	SealHandleTimeout time.Duration
	SealPollInterval  time.Duration
}

// The real, production Handler //
type AgentHandler struct {
	cfg HandlerConfig

	mu     sync.Mutex
	vaults map[string]*vault.Vault
}
