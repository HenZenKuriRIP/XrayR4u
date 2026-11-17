package anytls

import (
	"context"

	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
)

// xray-core v26.11.9 removed common/singbridge. AnyTLS still talks to
// sing-anytls, so these two adapters stay in this package.

type coreLogger struct{}

func newCoreLogger() logger.ContextLogger {
	return coreLogger{}
}

func (coreLogger) Trace(...any) {}
func (coreLogger) Debug(args ...any) {
	errors.LogDebug(context.Background(), args...)
}
func (coreLogger) Info(args ...any) {
	errors.LogInfo(context.Background(), args...)
}
func (coreLogger) Warn(args ...any) {
	errors.LogWarning(context.Background(), args...)
}
func (coreLogger) Error(args ...any) {
	errors.LogError(context.Background(), args...)
}
func (coreLogger) Fatal(...any)                         {}
func (coreLogger) Panic(...any)                         {}
func (coreLogger) TraceContext(context.Context, ...any) {}
func (coreLogger) DebugContext(ctx context.Context, args ...any) {
	errors.LogDebug(ctx, args...)
}
func (coreLogger) InfoContext(ctx context.Context, args ...any) {
	errors.LogInfo(ctx, args...)
}
func (coreLogger) WarnContext(ctx context.Context, args ...any) {
	errors.LogWarning(ctx, args...)
}
func (coreLogger) ErrorContext(ctx context.Context, args ...any) {
	errors.LogError(ctx, args...)
}
func (coreLogger) FatalContext(context.Context, ...any) {}
func (coreLogger) PanicContext(context.Context, ...any) {}

func socksaddrToDestination(socksaddr M.Socksaddr, network net.Network) (net.Destination, error) {
	if socksaddr.IsFqdn() {
		return net.Destination{
			Network: network,
			Address: net.DomainAddress(socksaddr.Fqdn),
			Port:    net.Port(socksaddr.Port),
		}, nil
	}
	if socksaddr.IsIP() {
		return net.Destination{
			Network: network,
			Address: net.IPAddress(socksaddr.Addr.AsSlice()),
			Port:    net.Port(socksaddr.Port),
		}, nil
	}
	return net.Destination{}, errors.New("invalid socks address: ", socksaddr)
}
