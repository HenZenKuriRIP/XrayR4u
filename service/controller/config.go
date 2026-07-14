package controller

type Config struct {
	ListenIP               string            `mapstructure:"ListenIP"`
	SendIP                 string            `mapstructure:"SendIP"`
	UpdatePeriodic         int               `mapstructure:"UpdatePeriodic"`
	CertConfig             *CertConfig       `mapstructure:"CertConfig"`
	EnableDNS              bool              `mapstructure:"EnableDNS"`
	DNSType                string            `mapstructure:"DNSType"`
	DisableUploadTraffic   bool              `mapstructure:"DisableUploadTraffic"`
	DisableGetRule         bool              `mapstructure:"DisableGetRule"`
	DisableReportNodeStatus bool             `mapstructure:"DisableReportNodeStatus"`
	EnableProxyProtocol    bool              `mapstructure:"EnableProxyProtocol"`
	EnableFallback         bool              `mapstructure:"EnableFallback"`
	DisableIVCheck         bool              `mapstructure:"DisableIVCheck"`
	DisableSniffing        bool              `mapstructure:"DisableSniffing"`
	FallBackConfigs        []*FallBackConfig `mapstructure:"FallBackConfigs"`
	AnyTLSPaddingScheme    string            `mapstructure:"AnyTLSPaddingScheme"`
	// AnyTLSFallback is the TCP address (host:port) of a real HTTPS server to
	// which unauthenticated connections are silently proxied. When set, the
	// AnyTLS port becomes indistinguishable from a normal TLS endpoint under
	// GFW active probing. Example: "127.0.0.1:8443".
	// When empty, unauthenticated connections are rejected immediately (a
	// detectable fingerprint — configure this in all anti-censorship deployments).
	AnyTLSFallback         string            `mapstructure:"AnyTLSFallback"`
}

type CertConfig struct {
	CertMode         string            `mapstructure:"CertMode"` // none, file, http, dns
	RejectUnknownSni bool              `mapstructure:"RejectUnknownSni"`
	CertDomain       string            `mapstructure:"CertDomain"`
	CertFile         string            `mapstructure:"CertFile"`
	KeyFile          string            `mapstructure:"KeyFile"`
	Provider         string            `mapstructure:"Provider"` // alidns, cloudflare, gandi, godaddy....
	Email            string            `mapstructure:"Email"`
	DNSEnv           map[string]string `mapstructure:"DNSEnv"`
}

type FallBackConfig struct {
	SNI              string `mapstructure:"SNI"`
	Alpn             string `mapstructure:"Alpn"`
	Path             string `mapstructure:"Path"`
	Dest             string `mapstructure:"Dest"`
	ProxyProtocolVer uint64 `mapstructure:"ProxyProtocolVer"`
}
