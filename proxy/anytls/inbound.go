package anytls

import (
	"context"
	"io"
	"net"
	"time"

	singanytls "github.com/HenZenKuriRIP/XrayR4u/proxy/anytls/lib"
	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls/lib/padding"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/singbridge"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/internet/stat"
)

const (
	// fallbackDialTimeout is the maximum time allowed to connect to the fallback
	// HTTPS server. Kept short so that a misconfigured fallback address does not
	// cause the probe handler to stall the goroutine pool.
	fallbackDialTimeout = 5 * time.Second
	// fallbackRelayTimeout bounds the full request/response copy so hung
	// clients or upstreams cannot pin goroutines and FDs forever (DoS).
	fallbackRelayTimeout = 30 * time.Second
)

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewServer(ctx, config.(*Config))
	}))
}

// Server is the anytls inbound handler that implements proxy.Inbound.
type Server struct {
	service    *singanytls.Service
	dispatcher routing.Dispatcher
}

// NewServer creates a new anytls Server from protobuf config.
func NewServer(ctx context.Context, config *Config) (*Server, error) {
	v := core.MustFromContext(ctx)
	dispatcher := v.GetFeature(routing.DispatcherType()).(routing.Dispatcher)

	// Use default padding scheme when none is provided. sing-anytls rejects
	// empty padding; an empty config.PaddingScheme would cause NewService to
	// return an "incorrect padding scheme format" error.
	scheme := config.PaddingScheme
	if len(scheme) == 0 {
		scheme = padding.DefaultPaddingScheme
	}

	s := &Server{
		dispatcher: dispatcher,
	}

	// Build the fallback handler. A nil fallback causes unauthenticated probes
	// to be rejected immediately, which is a clear active-probing fingerprint.
	// When FallbackAddr is configured, probes are silently forwarded to a real
	// HTTPS server, making this port indistinguishable from normal TLS traffic.
	var fallbackHandler N.TCPConnectionHandlerEx
	if addr := config.GetFallbackAddr(); addr != "" {
		fallbackHandler = &fallbackDialerHandler{addr: addr}
	}

	svcConfig := singanytls.ServiceConfig{
		PaddingScheme:   scheme,
		Users:           nil, // users are added later via UpdateUsers() from the controller
		Handler:         &tcpHandlerEx{server: s},
		Logger:          singbridge.NewLogger(errors.New),
		FallbackHandler: fallbackHandler,
	}

	svc, err := singanytls.NewService(svcConfig)
	if err != nil {
		return nil, err
	}
	s.service = svc
	return s, nil
}

// UpdateUsers replaces the full user table in the underlying sing-anytls service.
// Auth state lives entirely inside sing-anytls (atomic); no local copy is kept.
func (s *Server) UpdateUsers(users []User) {
	singUsers := make([]singanytls.User, len(users))
	for i, u := range users {
		singUsers[i] = singanytls.User{Name: u.Name, Password: u.Password}
	}
	s.service.UpdateUsers(singUsers)
}

// Network implements proxy.Inbound.
func (s *Server) Network() []xnet.Network {
	return []xnet.Network{xnet.Network_TCP}
}

// Process implements proxy.Inbound. It is called by xray-core for each new
// TCP connection after TLS has been terminated by the transport layer.
//
// The dispatcher is NOT reassigned here — it was already captured from the
// core.Instance in NewServer, and sing-anytls may create multiple concurrent
// streams (goroutines) that read s.dispatcher within NewConnectionEx. Writing
// it here without synchronisation would be a data race.
func (s *Server) Process(ctx context.Context, network xnet.Network, connection stat.Connection, dispatcher routing.Dispatcher) error {
	source := M.SocksaddrFromNet(connection.RemoteAddr())
	return s.service.NewConnection(ctx, connection, source, func(err error) {
		// onClose: connection terminated; nothing to clean up at this level.
		_ = err
	})
}

// tcpHandlerEx implements N.TCPConnectionHandlerEx to bridge sing-anytls
// callbacks into xray-core's DispatchLink.
type tcpHandlerEx struct {
	server *Server
}

// NewConnectionEx is called by sing-anytls after successful authentication,
// with the destination address parsed from the client's request.
//
// IMPORTANT: sing-anytls calls this concurrently from multiple goroutines
// (one per stream on the same physical TCP connection). The ctx passed here
// is the shared context from NewConnection → Process, which carries a single
// *session.Inbound pointer set by proxyman. We MUST NOT mutate that shared
// struct — instead we derive a fresh copy per stream to avoid a data race
// on inbound.User.
func (h *tcpHandlerEx) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	// Convert M.Socksaddr → xray-core net.Destination using xray-core's
	// own helper (handles FQDN, IP, and invalid addresses).
	dest, err := singbridge.ToDestination(destination, xnet.Network_TCP)
	if err != nil {
		errors.LogInfo(ctx, "anytls: invalid destination: ", err)
		if onClose != nil {
			onClose(err)
		} else {
			_ = conn.Close()
		}
		return
	}

	// Retrieve the authenticated user name from the context (set by sing-anytls).
	// The User.Name is in "tag|email|uid" format — compatible with the limiter's
	// emailKey and traffic stat counter names.
	userName, _ := auth.UserFromContext[string](ctx)

	// Derive a per-stream session.Inbound copy. The original (shared) inbound
	// was placed on ctx by proxyman with Tag, Source, and other fields set.
	// We preserve Tag and Source (needed by mydispatcher for IP tracking and
	// device limiting) and replace User with the per-stream value so concurrent
	// streams on the same physical connection do not race on inbound.User.
	// origInbound may be nil for abnormal contexts — never dereference blindly.
	newInbound := &session.Inbound{
		User: &protocol.MemoryUser{Email: userName},
	}
	if origInbound := session.InboundFromContext(ctx); origInbound != nil {
		newInbound.Tag = origInbound.Tag
		newInbound.Source = origInbound.Source
		newInbound.Local = origInbound.Local
	}
	ctx = session.ContextWithInbound(ctx, newInbound)

	// Wrap the sing-anytls stream as xray-core buf.Reader / buf.Writer.
	link := &transport.Link{
		Reader: buf.NewReader(conn),
		Writer: buf.NewWriter(conn),
	}

	if err := h.server.dispatcher.DispatchLink(ctx, dest, link); err != nil {
		// DispatchLink already closes the link on error.
		return
	}
}

// fallbackDialerHandler implements N.TCPConnectionHandlerEx. It is wired as
// the FallbackHandler of the sing-anytls Service and receives every connection
// that fails authentication (wrong or missing password prefix). By forwarding
// those connections to a real HTTPS server at addr, the AnyTLS port becomes
// indistinguishable from a normal TLS endpoint under GFW active probing —
// probes receive a genuine TLS ServerHello and certificate rather than a RST
// or immediate close, which is the fingerprint produced by a nil fallback.
type fallbackDialerHandler struct {
	addr string // e.g. "127.0.0.1:8443"
}

// NewConnectionEx dials addr and bidirectionally pipes bytes between the
// unauthenticated inbound conn and the upstream HTTPS server. The call returns
// immediately; the copy goroutines run in the background and close both sides
// when the transfer completes or either peer closes the connection.
func (h *fallbackDialerHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	upstream, err := net.DialTimeout("tcp", h.addr, fallbackDialTimeout)
	if err != nil {
		conn.Close()
		return
	}
	// Must block until the relay completes: if this method returns, NewConnection
	// returns, Process returns, and xray-core closes the underlying TCP connection
	// while the relay goroutines are still copying. Running synchronously here
	// keeps the TCP socket alive for the full lifetime of the fallback session.
	h.relay(conn, upstream, onClose)
}

// relay forwards the HTTP request cached in client to upstream (nginx),
// then copies nginx's response back to client. It deliberately does NOT use
// bidirectional io.Copy goroutines because the cached buffer creates a
// deadlock: after the buffer is drained, the client→upstream copy blocks on
// the underlying conn waiting for more data from curl, while curl is blocked
// waiting for a response. The solution is to extract the cached request,
// flush it synchronously, half-close upstream so nginx knows the request is
// complete, then copy the response back.
func (h *fallbackDialerHandler) relay(client, upstream net.Conn, onClose N.CloseHandlerFunc) {
	defer func() {
		client.Close()
		upstream.Close()
		if onClose != nil {
			onClose(nil)
		}
	}()

	deadline := time.Now().Add(fallbackRelayTimeout)
	_ = client.SetDeadline(deadline)
	_ = upstream.SetDeadline(deadline)

	// Extract the cached HTTP request from the CachedConn. sing-anytls
	// buffers the entire initial read and resets the cursor via b.Resize(0,n),
	// so the buffer replays the full original HTTP request.
	// After draining the buffer, do NOT try io.Copy(upstream, client) — the
	// CachedConn falls through to the underlying socket which blocks (curl is
	// waiting for a response, not sending more data).
	if cc, ok := client.(*bufio.CachedConn); ok {
		if cached := cc.ReadCached(); cached != nil {
			_, _ = upstream.Write(cached.Bytes())
			cached.Release()
		}
	}

	// Signal nginx that the request is complete. Without this half-close,
	// nginx waits indefinitely for a request body that never arrives.
	if tc, ok := upstream.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}

	// Copy nginx's HTTP response back to curl (through xray-core TLS).
	// Deadlines above ensure hung peers cannot pin this goroutine forever.
	_, _ = io.Copy(client, upstream)
}

// Interface compliance checks
var _ proxy.Inbound = (*Server)(nil)
var _ N.TCPConnectionHandlerEx = (*tcpHandlerEx)(nil)
var _ N.TCPConnectionHandlerEx = (*fallbackDialerHandler)(nil)
