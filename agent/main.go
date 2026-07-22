package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/IvanBez42/Portcullio/agent/internal/luks"
	"github.com/IvanBez42/Portcullio/agent/internal/socket"
)

const (
	inputDir     = "/lockers"
	mountAreaDir = "/mounts"
	socketPath = "/socket/agent.sock"
)

func main() {
	cfg := loadHandlerConfig()

	startupTimeout := getEnvDuration("PORTCULLIO_STARTUP_RECONCILE_TIMEOUT", 2*time.Second)
	startupPoll := getEnvDuration("PORTCULLIO_STARTUP_RECONCILE_POLL_INTERVAL", 100*time.Millisecond)

	if err := os.MkdirAll(cfg.InputDir, 0o700); err != nil {
		log.Fatalf("portcullio agent: create input dir %s: %v", cfg.InputDir, err)
	}
	if err := os.MkdirAll(cfg.MountAreaDir, 0o700); err != nil {
		log.Fatalf("portcullio agent: create mount area dir %s: %v", cfg.MountAreaDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		log.Fatalf("portcullio agent: create socket dir for %s: %v", socketPath, err)
	}

	if err := luks.EnsureLoopSupport(); err != nil {
		log.Printf("portcullio agent: warning: %v (vault create/unseal will fail until loop devices are available)", err)
	}

	handler := socket.NewAgentHandler(cfg)

	log.Println("portcullio agent: reconciling any vaults found in", cfg.InputDir)
	if err := handler.ReconcileAll(startupTimeout, startupPoll, func(msg string) {
		log.Println("portcullio agent: reconcile:", msg)
	}); err != nil {
		log.Fatalf("portcullio agent: startup reconciliation: %v", err)
	}

	srv, err := socket.NewServer(socketPath, handler)
	if err != nil {
		log.Fatalf("portcullio agent: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("portcullio agent: listening on", socketPath)
	select {
	case sig := <-sigCh:
		log.Println("portcullio agent: received", sig, "-- shutting down")
		if err := srv.Close(); err != nil {
			log.Printf("portcullio agent: close server: %v", err)
		}
		<-serveErr
	case err := <-serveErr:
		if err != nil {
			log.Fatalf("portcullio agent: serve: %v", err)
		}
	}
	log.Println("portcullio agent: shut down cleanly")
}

func loadHandlerConfig() socket.HandlerConfig {
	return socket.HandlerConfig{
		InputDir:          inputDir,
		MountAreaDir:      mountAreaDir,
		Fstype:            getEnv("PORTCULLIO_FSTYPE", "ext4"),
		SealHandleTimeout: getEnvDuration("PORTCULLIO_SEAL_HANDLE_TIMEOUT", 10*time.Second),
		SealPollInterval:  getEnvDuration("PORTCULLIO_SEAL_POLL_INTERVAL", 200*time.Millisecond),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("portcullio agent: invalid %s=%q: %v", key, v, err)
	}
	return d
}
