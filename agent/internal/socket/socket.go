package socket

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// The seven verbs //
const (
	VerbStatus   = "status"
	VerbUnseal   = "unseal"
	VerbSeal     = "seal"
	VerbCreate   = "create"
	VerbDestroy  = "destroy"
	VerbServices = "services"
	VerbSpace    = "space"
)

// Bounds a single request/response line //
const maxMessageBytes = 1 << 20 // 1MiB

// Marshals v as one JSON line to w //
func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("socket: marshal message: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("socket: write message: %w", err)
	}
	return nil
}

// Reads one JSON line from r into v //
func readMessage(r *bufio.Reader, v any) error {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("socket: read message: %w", err)
	}
	unmarshalErr := json.Unmarshal(line, v)
	zeroBytes(line)
	if unmarshalErr != nil {
		return fmt.Errorf("socket: decode message: %w", unmarshalErr)
	}
	return nil
}

// Overwrites b in place //
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// Bounds how long Server waits for a request/response line //
const ioDeadline = 30 * time.Second

// Creates the Unix socket at socketPath //
func NewServer(socketPath string, handler *AgentHandler) (*Server, error) {
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("socket: remove stale socket %s: %w", socketPath, err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("socket: listen on %s: %w", socketPath, err)
	}
	// Owner+group rw; cross-container ownership is a deploy concern //
	if err := os.Chmod(socketPath, 0o770); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("socket: chmod %s: %w", socketPath, err)
	}
	return &Server{ln: ln, handler: handler}, nil
}

// Accepts connections until Close //
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("socket: accept: %w", err)
		}
		go s.handleConn(conn)
	}
}

// Stops Serve //
func (s *Server) Close() error {
	return s.ln.Close()
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(ioDeadline)); err != nil {
		return
	}
	r := bufio.NewReader(io.LimitReader(conn, maxMessageBytes))

	var req Request
	if err := readMessage(r, &req); err != nil {
		_ = writeMessage(conn, Response{OK: false, Error: err.Error()})
		return
	}

	// Clear deadline: Handle may legitimately run long //
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return
	}

	resp := s.handler.Handle(req)

	if err := conn.SetWriteDeadline(time.Now().Add(ioDeadline)); err != nil {
		return
	}
	_ = writeMessage(conn, resp)
}

// Dials socketPath, sends req, returns the response -- the client half of
// this package's protocol, mirroring Server. No production caller today
// (main.go only ever runs a Server; ui is Node, so it speaks the protocol
// itself via agentClient.js). Used by every test in this package, and
// kept here rather than in a _test.go file because it depends on the
// unexported writeMessage/readMessage that Server also uses -- moving it
// would mean either duplicating that framing logic in test code or
// exporting those two just to avoid it, neither of which shrinks
// anything. //
func Call(ctx context.Context, socketPath string, req Request) (Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, fmt.Errorf("socket: dial %s: %w", socketPath, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if err := writeMessage(conn, req); err != nil {
		return Response{}, err
	}

	var resp Response
	r := bufio.NewReader(io.LimitReader(conn, maxMessageBytes))
	if err := readMessage(r, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

