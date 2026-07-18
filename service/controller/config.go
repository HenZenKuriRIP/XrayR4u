package controller

// Config is per-node controller configuration (config.yml → Nodes[].ControllerConfig).
// Local non-empty knobs override panel-derived NodeInfo so TLS+XHTTP+CDN+PQ
// can be fully driven on the node without panel changes.
type Config struct {
	ListenIP                string            `mapstructure:"ListenIP"`
	SendIP                  string            `mapstructure:"SendIP"`
	UpdatePeriodic          int               `mapstructure:"UpdatePeriodic"`
	CertConfig              *CertConfig       `mapstructure:"CertConfig"`
	EnableDNS               bool              `mapstructure:"EnableDNS"`
	DNSType                 string            `mapstructure:"DNSType"`
	DisableUploadTraffic    bool              `mapstructure:"DisableUploadTraffic"`
	DisableGetRule          bool              `mapstructure:"DisableGetRule"`
	DisableReportNodeStatus bool              `mapstructure:"DisableReportNodeStatus"`
	EnableProxyProtocol     bool              `mapstructure:"EnableProxyProtocol"`
	EnableFallback          bool              `mapstructure:"EnableFallback"`
	DisableIVCheck          bool              `mapstructure:"DisableIVCheck"`
	DisableSniffing         bool              `mapstructure:"DisableSniffing"`
	FallBackConfigs         []*FallBackConfig `mapstructure:"FallBackConfigs"`
	AnyTLSPaddingScheme     string            `mapstructure:"AnyTLSPaddingScheme"`
	// AnyTLSFallback is the TCP address (host:port) of a real HTTPS server to
	// which unauthenticated connections are silently proxied. When set, the
	// AnyTLS port becomes indistinguishable from a normal TLS endpoint under
	// GFW active probing. Example: "127.0.0.1:8443".
	// When empty, unauthenticated connections are rejected immediately (a
	// detectable fingerprint — configure this in all anti-censorship deployments).
	AnyTLSFallback string `mapstructure:"AnyTLSFallback"`

	// ---- REALITY / VLESS Encryption (existing PQ knobs) ----

	// RealityMinClientVer is the REALITY server minClientVer (x.y.z).
	// When non-empty it always overrides the value from the panel / code default.
	// When empty, panel tls_settings.min_client_ver is used if present; otherwise
	// the built-in default "1.8.0" is used.
	//
	// IMPORTANT (xray-core ≥ 26.7.11): if the final REALITY config leaves
	// minClientVer unset, the core defaults to "26.3.27" and many third-party
	// clients (e.g. Mihomo reporting 1.8.x) will fail authentication. Always
	// keep an explicit value unless you intentionally want the core default.
	RealityMinClientVer string `mapstructure:"RealityMinClientVer"`
	// RealityMldsa65Seed is the REALITY server-side ML-DSA-65 seed
	// (base64.RawURLEncoding, 32 bytes decoded). Non-empty value always
	// overrides panel tls_settings.mldsa65_seed / mldsa65Seed. Empty leaves
	// panel value (or off when panel also omits it). Generate with: xray mldsa65
	RealityMldsa65Seed string `mapstructure:"RealityMldsa65Seed"`
	// RealityShow enables REALITY debug logging (including whether
	// X25519MLKEM768 PQ key agreement was negotiated). Default false.
	RealityShow bool `mapstructure:"RealityShow"`
	// VlessDecryption is the VLESS inbound settings.decryption string.
	// Non-empty value always overrides panel "decryption". Empty uses panel
	// value, then defaults to "none".
	// Post-quantum VLESS Encryption example (server side):
	//   mlkem768x25519plus.native.0rtt.<padding>...<X25519_priv>.<MLKEM_seed>...
	// Generate key material with: xray mlkem768 / xray x25519
	// Cannot be used together with EnableFallback (xray-core constraint).
	// This is the primary PQ layer for TLS+XHTTP+CDN (REALITY PQ does not apply).
	VlessDecryption string `mapstructure:"VlessDecryption"`

	// ---- TLS + XHTTP + CDN local full control (panel-independent) ----

	// Transport overrides panel network when non-empty.
	// Examples: "tcp", "xhttp", "splithttp", "ws", "grpc".
	// For CDN deployments set "xhttp".
	Transport string `mapstructure:"Transport"`
	// Security overrides panel TLS type when non-empty.
	// Examples: "tls", "reality", "none".
	// For CDN origin Full/Strict set "tls"; for CDN-only TLS (origin plain) use "none".
	Security string `mapstructure:"Security"`
	// Vision controls XTLS Vision flow on VLESS users.
	//   "" / "auto" — use panel EnableVision, except XHTTP defaults to off when panel is silent
	//   "on"        — force xtls-rprx-vision
	//   "off"       — force no flow (recommended for pure CDN unless you know you need Vision)
	// Vision on XHTTP is supported by core with VLESS Encryption, but CDN middleboxes
	// often make Vision unnecessary; default auto-off for xhttp is safer.
	Vision string `mapstructure:"Vision"`
	// XHTTP holds server-side XHTTP (splithttp) settings. Local non-empty
	// Host/Path/Mode override panel. Required for a complete CDN path when
	// the panel does not supply host/path.
	XHTTP *XHTTPConfig `mapstructure:"XHTTP"`
}

// XHTTPConfig is the node-local XHTTP / SplitHTTP inbound profile.
// Maps to xray-core conf.SplitHTTPConfig (server side).
type XHTTPConfig struct {
	// Host is the HTTP Host / authority expected on the origin (often the CDN domain).
	Host string `mapstructure:"Host"`
	// Path is the URL path (must start with /). Required for CDN routing.
	Path string `mapstructure:"Path"`
	// Mode: auto | packet-up | stream-up | stream-one. Empty → auto.
	// CDN: "auto" or "stream-up" are common; stream-one needs HTTP/2 end-to-end.
	Mode string `mapstructure:"Mode"`
	// Headers are extra response/request headers (must NOT include Host).
	Headers map[string]string `mapstructure:"Headers"`
	// NoSSEHeader disables SSE content-type heuristics when needed for CDN quirks.
	NoSSEHeader bool `mapstructure:"NoSSEHeader"`
	// NoGRPCHeader disables gRPC framing hints.
	NoGRPCHeader bool `mapstructure:"NoGRPCHeader"`
	// Extra is optional raw JSON merged into xray-core SplitHTTPConfig for advanced
	// CDN knobs (xPaddingBytes, xmux, scMaxEachPostBytes, sessionID*, etc.).
	// Host/Path/Mode from this struct still take precedence over Extra.
	Extra string `mapstructure:"Extra"`
}

// CertConfig controls origin TLS certificates and TLS PQ/handshake options.
type CertConfig struct {
	CertMode         string            `mapstructure:"CertMode"` // none, file, http, dns
	RejectUnknownSni bool              `mapstructure:"RejectUnknownSni"`
	CertDomain       string            `mapstructure:"CertDomain"`
	CertFile         string            `mapstructure:"CertFile"`
	KeyFile          string            `mapstructure:"KeyFile"`
	Provider         string            `mapstructure:"Provider"` // alidns, cloudflare, gandi, godaddy....
	Email            string            `mapstructure:"Email"`
	DNSEnv           map[string]string `mapstructure:"DNSEnv"`

	// ALPN is the TLS ALPN list. Empty defaults to ["h2","http/1.1"] for XHTTP/CDN.
	ALPN []string `mapstructure:"ALPN"`
	// MinVersion / MaxVersion e.g. "1.2", "1.3". Empty uses core defaults.
	MinVersion string `mapstructure:"MinVersion"`
	MaxVersion string `mapstructure:"MaxVersion"`
	// CurvePreferences controls TLS key-exchange groups.
	// Empty defaults to PQ-ready: ["X25519MLKEM768","X25519","P256"] when TLS is used.
	// Set explicitly to disable PQ curves if needed (e.g. ["X25519","P256"]).
	CurvePreferences []string `mapstructure:"CurvePreferences"`
	// EnableSessionResumption enables TLS session tickets (default false).
	EnableSessionResumption bool `mapstructure:"EnableSessionResumption"`
	// ECHServerKeys is optional base64 ECH private keys for TLS ECH (advanced).
	ECHServerKeys string `mapstructure:"ECHServerKeys"`
}

type FallBackConfig struct {
	SNI              string `mapstructure:"SNI"`
	Alpn             string `mapstructure:"Alpn"`
	Path             string `mapstructure:"Path"`
	Dest             string `mapstructure:"Dest"`
	ProxyProtocolVer uint64 `mapstructure:"ProxyProtocolVer"`
}
