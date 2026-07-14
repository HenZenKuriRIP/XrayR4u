package mydispatcher

//go:generate go run github.com/xtls/xray-core/common/errors/errorgen

import (
	"context"
	"fmt"

	"strings"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/common/limiter"
	"github.com/HenZenKuriRIP/XrayR4u/common/rule"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	xlog "github.com/xtls/xray-core/common/log"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/routing"
	routing_session "github.com/xtls/xray-core/features/routing/session"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/pipe"
)

var errSniffingTimeout = newError("timeout on sniffing")

type cachedReader struct {
	sync.Mutex
	reader *pipe.Reader
	cache  buf.MultiBuffer
}

func (r *cachedReader) Cache(b *buf.Buffer) {
	mb, _ := r.reader.ReadMultiBufferTimeout(time.Millisecond * 100)
	r.Lock()
	if !mb.IsEmpty() {
		r.cache, _ = buf.MergeMulti(r.cache, mb)
	}
	b.Clear()
	rawBytes := b.Extend(buf.Size)
	n := r.cache.Copy(rawBytes)
	b.Resize(0, int32(n))
	r.Unlock()
}

func (r *cachedReader) readInternal() buf.MultiBuffer {
	r.Lock()
	defer r.Unlock()

	if r.cache != nil && !r.cache.IsEmpty() {
		mb := r.cache
		r.cache = nil
		return mb
	}

	return nil
}

func (r *cachedReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	return r.reader.ReadMultiBuffer()
}

func (r *cachedReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb := r.readInternal()
	if mb != nil {
		return mb, nil
	}

	return r.reader.ReadMultiBufferTimeout(timeout)
}

func (r *cachedReader) Interrupt() {
	r.Lock()
	if r.cache != nil {
		r.cache = buf.ReleaseMulti(r.cache)
	}
	r.Unlock()
	r.reader.Interrupt()
}

// connTrackWriter wraps a buf.Writer and calls onClose exactly once when the
// connection ends (via either Close or Interrupt). It is used to maintain
// accurate online IP counts through Xray-core's stats.OnlineMap.
type connTrackWriter struct {
	buf.Writer
	once    sync.Once
	onClose func()
}

func (w *connTrackWriter) Close() error {
	w.once.Do(w.onClose)
	return common.Close(w.Writer)
}

func (w *connTrackWriter) Interrupt() {
	w.once.Do(w.onClose)
	common.Interrupt(w.Writer)
}

// DefaultDispatcher is a default implementation of Dispatcher.
type DefaultDispatcher struct {
	ohm         outbound.Manager
	router      routing.Router
	policy      policy.Manager
	stats       stats.Manager
	Limiter     *limiter.Limiter
	RuleManager *rule.RuleManager
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		d := new(DefaultDispatcher)
		if err := core.RequireFeatures(ctx, func(om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dc dns.Client) error {
			return d.Init(config.(*Config), om, router, pm, sm, dc)
		}); err != nil {
			return nil, err
		}
		return d, nil
	}))
}

// Init initializes DefaultDispatcher.
func (d *DefaultDispatcher) Init(config *Config, om outbound.Manager, router routing.Router, pm policy.Manager, sm stats.Manager, dc dns.Client) error {
	d.ohm = om
	d.router = router
	d.policy = pm
	d.stats = sm
	d.Limiter = limiter.New()
	d.RuleManager = rule.New()
	return nil
}

// Type implements common.HasType.
func (*DefaultDispatcher) Type() interface{} {
	return routing.DispatcherType()
}

// Start implements common.Runnable.
func (*DefaultDispatcher) Start() error {
	return nil
}

// Close implements common.Closable.
func (*DefaultDispatcher) Close() error { return nil }

// onlineMapName returns the Xray-core stats.OnlineMap name for a given inbound tag.
func onlineMapName(tag string) string {
	return "inbound>>>" + tag + ">>>online>>>ip"
}

// sourceIP safely returns the string representation of the source IP from an
// inbound connection. It returns an empty string when inbound or its Source.Address
// is nil, avoiding a nil-interface panic on Address.IP().
func sourceIP(inbound *session.Inbound) string {
	if inbound == nil || inbound.Source.Address == nil {
		return ""
	}
	return inbound.Source.Address.IP().String()
}

// trackConnection registers a new active connection in the OnlineMap and wraps
// the link's Writer with a connTrackWriter that calls RemoveIP on close/interrupt.
// When email is non-empty the onClose callback also removes the IP from the
// limiter's per-user online state so device-limit slots are freed immediately.
func (d *DefaultDispatcher) trackConnection(tag string, ip string, email string, link *transport.Link) {
	om, err := stats.GetOrRegisterOnlineMap(d.stats, onlineMapName(tag))
	if om == nil {
		errors.LogDebug(context.Background(), "[conn-track] GetOrRegisterOnlineMap failed tag=", tag, " ip=", ip, " err=", err)
		return
	}
	om.AddIP(ip)
	errors.LogDebug(context.Background(), "[conn-track] AddIP tag=", tag, " ip=", ip, " count=", om.Count())
	link.Writer = &connTrackWriter{
		Writer: link.Writer,
		onClose: func() {
			om.RemoveIP(ip)
			if email != "" {
				d.Limiter.RemoveOnlineIP(tag, email, ip)
			}
			errors.LogDebug(context.Background(), "[conn-track] RemoveIP tag=", tag, " ip=", ip, " count=", om.Count())
		},
	}
}

// GetOnlineIPCount returns the current number of unique online IPs for the
// given inbound tag. It returns 0 when no OnlineMap has been registered yet.
func (d *DefaultDispatcher) GetOnlineIPCount(tag string) int {
	name := onlineMapName(tag)
	om := d.stats.GetOnlineMap(name)
	if om != nil {
		count := om.Count()
		return count
	}
	// OnlineMap not yet registered (no connections established yet),
	// which means zero online IPs — not an error condition.
	return 0
}

func (d *DefaultDispatcher) getLink(ctx context.Context) (*transport.Link, *transport.Link, error) {
	opt := pipe.OptionsFromContext(ctx)
	uplinkReader, uplinkWriter := pipe.New(opt...)
	downlinkReader, downlinkWriter := pipe.New(opt...)

	inboundLink := &transport.Link{
		Reader: downlinkReader,
		Writer: uplinkWriter,
	}

	outboundLink := &transport.Link{
		Reader: uplinkReader,
		Writer: downlinkWriter,
	}

	sessionInbound := session.InboundFromContext(ctx)
	var user *protocol.MemoryUser
	if sessionInbound != nil {
		// Force userland copy path for all protocols (same as DispatchLink)
		sessionInbound.CanSpliceCopy = 3
		user = sessionInbound.User

		// Track active connection via Xray-core OnlineMap (refcount-based).
		// Only track connections that have a real source IP (skip internal
		// dispatches like DNS DoH where Source may be unset).
		if sessionInbound.Source.Address != nil {
			userEmail := ""
			if user != nil {
				userEmail = user.Email
			}
			d.trackConnection(sessionInbound.Tag, sessionInbound.Source.Address.IP().String(), userEmail, inboundLink)
		}
	}

	if user != nil && len(user.Email) > 0 {
		// Speed Limit and Device Limit
		bucket, ok, reject := d.Limiter.GetUserBucket(sessionInbound.Tag, user.Email, sourceIP(sessionInbound))
		if reject {
			errors.LogError(context.Background(), "Devices reach the limit: ", user.Email)
			common.Close(outboundLink.Writer)
			common.Close(inboundLink.Writer)
			common.Interrupt(outboundLink.Reader)
			common.Interrupt(inboundLink.Reader)
			return nil, nil, newError("Devices reach the limit: ", user.Email)
		}
		if ok {
			inboundLink.Writer = d.Limiter.RateWriter(inboundLink.Writer, bucket)
			outboundLink.Writer = d.Limiter.RateWriter(outboundLink.Writer, bucket)
		}
		p := d.policy.ForLevel(user.Level)
		if p.Stats.UserUplink {
			name := "user>>>" + user.Email + ">>>traffic>>>uplink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				inboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  inboundLink.Writer,
				}
			}
		}
		if p.Stats.UserDownlink {
			name := "user>>>" + user.Email + ">>>traffic>>>downlink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				outboundLink.Writer = &SizeStatWriter{
					Counter: c,
					Writer:  outboundLink.Writer,
				}
			}
		}
	}

	return inboundLink, outboundLink, nil
}

func shouldOverride(ctx context.Context, result SniffResult, request session.SniffingRequest, destination net.Destination) bool {
	domain := result.Domain()
	for _, d := range request.ExcludeForDomain {
		if strings.ToLower(domain) == d {
			return false
		}
	}
	var fakeDNSEngine dns.FakeDNSEngine
	core.RequireFeatures(ctx, func(fdns dns.FakeDNSEngine) {
		fakeDNSEngine = fdns
	})
	protocolString := result.Protocol()
	if resComp, ok := result.(SnifferResultComposite); ok {
		protocolString = resComp.ProtocolForDomainResult()
	}
	for _, p := range request.OverrideDestinationForProtocol {
		if strings.HasPrefix(protocolString, p) {
			return true
		}
		if fkr0, ok := fakeDNSEngine.(dns.FakeDNSEngineRev0); ok && protocolString != "bittorrent" && p == "fakedns" &&
			destination.Address.Family().IsIP() && fkr0.IsIPInIPPool(destination.Address) {
			errors.LogInfo(ctx, "Using sniffer ", protocolString, " since the fake DNS missed")
			return true
		}
		if resultSubset, ok := result.(SnifferIsProtoSubsetOf); ok {
			if resultSubset.IsProtoSubsetOf(p) {
				return true
			}
		}
	}

	return false
}

// Dispatch implements routing.Dispatcher.
func (d *DefaultDispatcher) Dispatch(ctx context.Context, destination net.Destination) (*transport.Link, error) {
	if !destination.IsValid() {
		panic("Dispatcher: Invalid destination.")
	}
	ob := &session.Outbound{
		Target: destination,
	}
	ctx = session.ContextWithOutbounds(ctx, []*session.Outbound{ob})

	inbound, outbound, err := d.getLink(ctx)
	if err != nil {
		return nil, err
	}
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}
	sniffingRequest := content.SniffingRequest
	switch {
	case !sniffingRequest.Enabled:
		go d.routedDispatch(ctx, outbound, destination)
	case destination.Network != net.Network_TCP:
		// Only metadata sniff will be used for non tcp connection
		result, err := sniffer(ctx, nil, true)
		if err == nil {
			content.Protocol = result.Protocol()
			if shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
		}
		go d.routedDispatch(ctx, outbound, destination)
	default:
		// Guard the type assertion: Dispatch() is used by VMess/Shadowsocks/Trojan
		// inbounds where outbound.Reader is always a *pipe.Reader.
		// If it is not (e.g. a future protocol wraps the reader before calling
		// Dispatch), skip sniffing and route directly to avoid a panic.
		pipeReader, ok := outbound.Reader.(*pipe.Reader)
		if !ok {
			errors.LogWarning(ctx, "Dispatch: outbound.Reader is not *pipe.Reader, skipping sniff for [", destination, "]")
			go d.routedDispatch(ctx, outbound, destination)
			break
		}
		go func() {
			cReader := &cachedReader{
				reader: pipeReader,
			}
			outbound.Reader = cReader
			result, err := sniffer(ctx, cReader, sniffingRequest.MetadataOnly)
			if err == nil {
				content.Protocol = result.Protocol()
			}
			if err == nil && shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
			d.routedDispatch(ctx, outbound, destination)
		}()
	}
	return inbound, nil
}

// DispatchLink implements routing.Dispatcher.
func (d *DefaultDispatcher) DispatchLink(ctx context.Context, destination net.Destination, outbound *transport.Link) error {
	if !destination.IsValid() {
		return newError("Dispatcher: Invalid destination.")
	}
	ob := &session.Outbound{
		Target: destination,
	}
	ctx = session.ContextWithOutbounds(ctx, []*session.Outbound{ob})
	content := session.ContentFromContext(ctx)
	if content == nil {
		content = new(session.Content)
		ctx = session.ContextWithContent(ctx, content)
	}

	// Wrap the outbound link with per-user traffic stats counters and limiter.
	// VLESS (and Trojan) inbound uses DispatchLink which bypasses getLink(),
	// where stats wrapping and device/speed limiting normally happen for
	// protocols using Dispatch(). Without this:
	//   - getTraffic() always returns 0 → no traffic reported
	//   - GetOnlineDevice() returns empty → no device/online-user reporting
	sessionInbound := session.InboundFromContext(ctx)
	var user *protocol.MemoryUser
	if sessionInbound != nil {
		// Force userland copy path — disable kernel splice to ensure
		// SizeStatReader/SizeStatWriter count ALL traffic.
		// Without this, splice bypasses userland stats entirely
		// (CopyRawConnIfExist type-asserts *dispatcher.SizeStatWriter,
		// NOT *mydispatcher.SizeStatWriter).
		sessionInbound.CanSpliceCopy = 3
		user = sessionInbound.User

		// Track active connection via Xray-core OnlineMap (refcount-based).
		// Only track connections that have a real source IP.
		if sessionInbound.Source.Address != nil {
			userEmail := ""
			if user != nil {
				userEmail = user.Email
			}
			d.trackConnection(sessionInbound.Tag, sessionInbound.Source.Address.IP().String(), userEmail, outbound)
		}
	}
	if user != nil && len(user.Email) > 0 {
		// Speed Limit and Device Limit (same as getLink)
		bucket, ok, reject := d.Limiter.GetUserBucket(sessionInbound.Tag, user.Email, sourceIP(sessionInbound))
		if reject {
			common.Close(outbound.Writer)
			common.Interrupt(outbound.Reader)
			return newError("Devices reach the limit: ", user.Email)
		}
		if ok {
			outbound.Writer = d.Limiter.RateWriter(outbound.Writer, bucket)
			outbound.Reader = d.Limiter.RateReader(outbound.Reader, bucket)
		}
		// Per-user traffic stats counters
		p := d.policy.ForLevel(user.Level)
		if p.Stats.UserUplink {
			name := "user>>>" + user.Email + ">>>traffic>>>uplink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				outbound.Reader = &SizeStatReader{Counter: c, Reader: outbound.Reader}
			}
		}
		if p.Stats.UserDownlink {
			name := "user>>>" + user.Email + ">>>traffic>>>downlink"
			if c, _ := stats.GetOrRegisterCounter(d.stats, name); c != nil {
				outbound.Writer = &SizeStatWriter{Counter: c, Writer: outbound.Writer}
			}
		}
	}

	sniffingRequest := content.SniffingRequest
	switch {
	case !sniffingRequest.Enabled:
		go d.routedDispatch(ctx, outbound, destination)
	case destination.Network != net.Network_TCP:
		// Only metadata sniff will be used for non tcp connection
		result, err := sniffer(ctx, nil, true)
		if err == nil {
			content.Protocol = result.Protocol()
			if shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
		}
		go d.routedDispatch(ctx, outbound, destination)
	default:
		// VLESS+Vision (xtls-rprx-vision) / REALITY flow wraps the reader in
		// *proxy.VisionReader before calling DispatchLink. Two issues apply:
		//
		// 1. Content sniffing requires a *pipe.Reader; VisionReader would panic.
		//    Skip sniffing and route directly.
		//
		// 2. Context lifetime: the VLESS inbound Process() goroutine returns
		//    immediately after calling DispatchLink (Vision hands off the
		//    connection via VisionReader), triggering defer cancel(). This
		//    cancels ctx and kills the freedom proxy's DNS lookup / TCP dial
		//    with "operation was canceled". Fix: use WithoutCancel so the
		//    outbound goroutines are not tied to the inbound goroutine's exit.
		//
		// 3. TCP connection lifetime: the underlying TCP socket is closed by
		//    proxyman/inbound as soon as Process() returns. For Vision/REALITY
		//    streams (where outbound.Reader is a VisionReader, not a *pipe.Reader)
		//    we MUST run routedDispatch synchronously (not in a goroutine) so
		//    that Process() remains blocked, keeping the TCP connection alive
		//    for the entire session. Running it in a goroutine would cause
		//    DispatchLink to return immediately → Process() returns → TCP closed
		//    → "read tcp: use of closed network connection".
		pipeReader, ok := outbound.Reader.(*pipe.Reader)
		if !ok {
			// Vision/REALITY stream: run synchronously to keep TCP alive.
			// detachedCtx ensures outbound goroutines inside routedDispatch are
			// not cancelled by the inbound context (e.g. policy timeout cancel).
			detachedCtx := context.WithoutCancel(ctx)
			errors.LogInfo(ctx, "DispatchLink: Vision/REALITY stream detected, routing directly to [", destination, "]")
			d.routedDispatch(detachedCtx, outbound, destination)
			break
		}
		go func() {
			cReader := &cachedReader{
				reader: pipeReader,
			}
			outbound.Reader = cReader
			result, err := sniffer(ctx, cReader, sniffingRequest.MetadataOnly)
			if err == nil {
				content.Protocol = result.Protocol()
			}
			if err == nil && shouldOverride(ctx, result, sniffingRequest, destination) {
				domain := result.Domain()
				errors.LogInfo(ctx, "sniffed domain: ", domain)
				destination.Address = net.ParseAddress(domain)
				if sniffingRequest.RouteOnly && result.Protocol() != "fakedns" {
					ob.RouteTarget = destination
				} else {
					ob.Target = destination
				}
			}
			d.routedDispatch(ctx, outbound, destination)
		}()
	}
	return nil
}

func sniffer(ctx context.Context, cReader *cachedReader, metadataOnly bool) (SniffResult, error) {
	payload := buf.New()
	defer payload.Release()

	sniffer := NewSniffer(ctx)

	metaresult, metadataErr := sniffer.SniffMetadata(ctx)

	if metadataOnly {
		return metaresult, metadataErr
	}

	contentResult, contentErr := func() (SniffResult, error) {
		totalAttempt := 0
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				totalAttempt++
				if totalAttempt > 2 {
					return nil, errSniffingTimeout
				}

				cReader.Cache(payload)
				if !payload.IsEmpty() {
					result, err := sniffer.Sniff(ctx, payload.Bytes())
					if err != common.ErrNoClue {
						return result, err
					}
				}
				if payload.IsFull() {
					return nil, errUnknownContent
				}
			}
		}
	}()
	if contentErr != nil && metadataErr == nil {
		return metaresult, nil
	}
	if contentErr == nil && metadataErr == nil {
		return CompositeResult(metaresult, contentResult), nil
	}
	return contentResult, contentErr
}

func (d *DefaultDispatcher) routedDispatch(ctx context.Context, link *transport.Link, destination net.Destination) {
	var ob *session.Outbound
	if obs := session.OutboundsFromContext(ctx); len(obs) > 0 {
		ob = obs[0]
	}
	if ob == nil {
		ob = &session.Outbound{}
	}
	var handler outbound.Handler

	// Check if domain and protocol hit the rule
	sessionInbound := session.InboundFromContext(ctx)
	// Guard nil inbound (internal/DNS dispatches may lack one) before User access.
	if sessionInbound != nil && sessionInbound.User != nil {
		if d.RuleManager.Detect(sessionInbound.Tag, destination.String(), sessionInbound.User.Email) {
			errors.LogError(context.Background(), fmt.Sprintf("User %s access %s reject by rule", sessionInbound.User.Email, destination.String()))
			newError("destination is reject by rule")
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	}

	routingLink := routing_session.AsRoutingContext(ctx)
	inTag := routingLink.GetInboundTag()

	if forcedOutboundTag := session.GetForcedOutboundTagFromContext(ctx); forcedOutboundTag != "" {
		ctx = session.SetForcedOutboundTagToContext(ctx, "")
		if h := d.ohm.GetHandler(forcedOutboundTag); h != nil {
			errors.LogInfo(ctx, "taking platform initialized detour [", forcedOutboundTag, "] for [", destination, "]")
			handler = h
		} else {
			errors.LogError(ctx, "non existing tag for platform initialized detour: ", forcedOutboundTag)
			common.Close(link.Writer)
			common.Interrupt(link.Reader)
			return
		}
	} else if d.router != nil {
		if route, err := d.router.PickRoute(routing_session.AsRoutingContext(ctx)); err == nil {
			tag := route.GetOutboundTag()
			if h := d.ohm.GetHandler(tag); h != nil {
				errors.LogInfo(ctx, "taking detour [", tag, "] for [", destination, "]")
				handler = h
			} else {
				errors.LogWarning(ctx, "non existing outTag: ", tag, ", falling back to inbound tag: ", inTag)
			}
		} else {
			errors.LogInfo(ctx, "default route for ", destination)
		}
	}

	if handler == nil {
		// Try to use outbound handler with the same tag as the inbound
		if h := d.ohm.GetHandler(inTag); h != nil {
			errors.LogInfo(ctx, "using outbound handler matching inbound tag [", inTag, "] for [", destination, "]")
			handler = h
		}
	}

	// If there is no outbound with tag as same as the inbound tag
	if handler == nil {
		errors.LogInfo(ctx, "no outbound tag [", inTag, "] found, using default outbound handler for [", destination, "]")
		handler = d.ohm.GetDefaultHandler()
	}

	if handler == nil {
		errors.LogError(ctx, "no outbound handler available for [", destination, "] (inTag=", inTag, ") — traffic dropped!")
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return
	}

	if accessMessage := xlog.AccessMessageFromContext(ctx); accessMessage != nil {
		if tag := handler.Tag(); tag != "" {
			accessMessage.Detour = tag
		}
		xlog.Record(accessMessage)
	}

	handler.Dispatch(ctx, link)
}

