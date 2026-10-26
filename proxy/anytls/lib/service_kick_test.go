package anytls

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls/lib/padding"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type nopHandler struct{}

func (nopHandler) NewConnectionEx(context.Context, net.Conn, M.Socksaddr, M.Socksaddr, N.CloseHandlerFunc) {
}

type nopLogger struct{}

func (nopLogger) TraceContext(context.Context, ...any) {}
func (nopLogger) DebugContext(context.Context, ...any) {}
func (nopLogger) InfoContext(context.Context, ...any)  {}
func (nopLogger) WarnContext(context.Context, ...any)  {}
func (nopLogger) ErrorContext(context.Context, ...any) {}
func (nopLogger) FatalContext(context.Context, ...any) {}
func (nopLogger) PanicContext(context.Context, ...any) {}
func (nopLogger) Trace(...any)                         {}
func (nopLogger) Debug(...any)                         {}
func (nopLogger) Info(...any)                          {}
func (nopLogger) Warn(...any)                          {}
func (nopLogger) Error(...any)                         {}
func (nopLogger) Fatal(...any)                         {}
func (nopLogger) Panic(...any)                         {}

var _ logger.ContextLogger = nopLogger{}

func TestUpdateUsers_KicksRevokedSession(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		PaddingScheme: padding.DefaultPaddingScheme,
		Users:         []User{{Name: "u1", Password: "pass-one"}},
		Handler:       nopHandler{},
		Logger:        nopLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}

	client, server := net.Pipe()
	defer client.Close()

	// Authenticate handshake bytes: sha256(password) + paddingLen(0)
	hash := sha256.Sum256([]byte("pass-one"))
	frame := append(hash[:], 0, 0)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.NewConnection(context.Background(), server, M.Socksaddr{}, nil)
	}()

	if _, err := client.Write(frame); err != nil {
		t.Fatal(err)
	}

	// Wait until the session is tracked.
	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.sessionsMu.Lock()
		n := len(svc.sessions)
		svc.sessionsMu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session was never tracked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drop the user — live session must be closed.
	svc.UpdateUsers(nil)

	// Read from client should eventually fail (EOF / closed).
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	_, err = client.Read(buf)
	if err == nil {
		t.Fatal("expected read error after session kick")
	}
	if err != io.EOF && err != io.ErrClosedPipe {
		t.Logf("kick closed with: %v", err)
	}

	wg.Wait()

	svc.sessionsMu.Lock()
	left := len(svc.sessions)
	svc.sessionsMu.Unlock()
	if left != 0 {
		t.Fatalf("expected no tracked sessions after kick, got %d", left)
	}
}
