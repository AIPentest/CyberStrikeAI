package egress

import "sync"

// The package keeps a process-wide default Gateway so that the many outbound
// HTTP clients scattered across the codebase can route through the operator's
// anonymizing proxy without every constructor having to thread a *Gateway
// argument. The application sets it once at startup via SetDefault; consumers
// read it via Default.
var (
	defaultMu      sync.RWMutex
	defaultGateway *Gateway
)

// SetDefault installs the process-wide default gateway. Called once during
// application startup after the gateway is built from configuration.
func SetDefault(g *Gateway) {
	defaultMu.Lock()
	defaultGateway = g
	defaultMu.Unlock()
}

// Default returns the process-wide gateway. When none has been installed it
// returns a disabled passthrough gateway, so callers can unconditionally do
// egress.Default().Transport() without a nil check: before SetDefault runs,
// or in tests, traffic simply flows directly as it did before.
func Default() *Gateway {
	defaultMu.RLock()
	g := defaultGateway
	defaultMu.RUnlock()
	if g != nil {
		return g
	}
	return disabledGateway
}

// disabledGateway is the fallback returned by Default before SetDefault runs.
var disabledGateway = &Gateway{mode: ModeDirect, enabled: false}
