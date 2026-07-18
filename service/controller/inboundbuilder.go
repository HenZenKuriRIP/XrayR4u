// Package generate the InbounderConfig used by add inbound
package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/common/legocmd"
	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls"
	"github.com/xtls/xray-core/app/proxyman"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

// defaultRealityMinClientVer is used when neither config.yml nor panel
// supplies minClientVer. Must stay non-empty for xray-core ≥ 26.7.11.
const defaultRealityMinClientVer = "1.8.0"

// InboundBuilder build Inbound config for different protocol
func InboundBuilder(config *Config, nodeInfo *api.NodeInfo, tag string) (*core.InboundHandlerConfig, error) {
	inboundDetourConfig := &conf.InboundDetourConfig{}
	// Build Listen IP address
	if config.ListenIP != "" {
		ipAddress := xnet.ParseAddress(config.ListenIP)
		inboundDetourConfig.ListenOn = &conf.Address{ipAddress}
	}

	// Build Port
	portList := &conf.PortList{
		Range: []conf.PortRange{{From: uint32(nodeInfo.Port), To: uint32(nodeInfo.Port)}},
	}
	inboundDetourConfig.PortList = portList
	// Build Tag
	inboundDetourConfig.Tag = tag
	// SniffingConfig
	sniffingConfig := &conf.SniffingConfig{
		Enabled:      true,
		DestOverride: conf.StringList{"http", "tls"},
	}
	if config.DisableSniffing {
		sniffingConfig.Enabled = false
	}
	inboundDetourConfig.SniffingConfig = sniffingConfig

	// Build streamSettings (shared across protocols)
	streamSetting, err := buildStreamSetting(config, nodeInfo)
	if err != nil {
		return nil, err
	}

	// ---- Protocol selection ----
	nodeType := strings.ToLower(nodeInfo.NodeType)
	if nodeType == "anytls" {
		return buildAnyTLSHandlerConfig(config, nodeInfo, tag, streamSetting, sniffingConfig)
	}

	// Build Protocol — VLESS
	protocol := "vless"
	decryption := resolveVlessDecryption(config, nodeInfo)
	var (
		proxySetting interface{}
		setting      json.RawMessage
	)
	if config.EnableFallback {
		// xray-core forbids fallbacks together with non-"none" VLESS Encryption.
		if decryption != "none" {
			return nil, fmt.Errorf(
				"VLESS Encryption (decryption=%q) cannot be used together with EnableFallback; "+
					"disable FallBackConfigs or set decryption to none",
				decryption,
			)
		}
		fallbackConfigs, err := buildVlessFallbacks(config.FallBackConfigs)
		if err != nil {
			return nil, err
		}
		proxySetting = &conf.VLessInboundConfig{
			Decryption: decryption,
			Fallbacks:  fallbackConfigs,
		}
	} else {
		proxySetting = &conf.VLessInboundConfig{
			Decryption: decryption,
		}
	}

	setting, err = json.Marshal(proxySetting)
	if err != nil {
		return nil, fmt.Errorf("Marshal proxy %s config failed: %s", nodeInfo.NodeType, err)
	}

	inboundDetourConfig.Protocol = protocol
	inboundDetourConfig.StreamSetting = streamSetting
	inboundDetourConfig.Settings = &setting

	return inboundDetourConfig.Build()
}

// resolveVlessDecryption picks the VLESS inbound decryption string.
// Priority: ControllerConfig.VlessDecryption > panel NodeInfo.VlessDecryption > "none".
func resolveVlessDecryption(config *Config, nodeInfo *api.NodeInfo) string {
	if config != nil {
		if v := strings.TrimSpace(config.VlessDecryption); v != "" {
			return v
		}
	}
	if nodeInfo != nil {
		if v := strings.TrimSpace(nodeInfo.VlessDecryption); v != "" {
			return v
		}
	}
	return "none"
}

// buildStreamSetting constructs the stream/tls/transport settings shared by
// all protocols. It is extracted from InboundBuilder so anytls can reuse it
// without going through conf.InboundDetourConfig.Build().
//
// Local ControllerConfig can fully drive TLS+XHTTP+CDN without panel fields:
//   Transport, Security, XHTTP.*, CertConfig (ALPN/curves), EnableProxyProtocol.
func buildStreamSetting(config *Config, nodeInfo *api.NodeInfo) (*conf.StreamConfig, error) {
	streamSetting := new(conf.StreamConfig)
	transportProtocolName := resolveTransport(config, nodeInfo)
	transportProtocol := conf.TransportProtocol(transportProtocolName)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}

	// Keep Network as the user-facing name (xhttp) when possible; Build() maps it.
	streamSetting.Network = &transportProtocol

	switch networkType {
	case "tcp":
		var header json.RawMessage
		if nodeInfo != nil {
			header = nodeInfo.Header
		}
		streamSetting.TCPSettings = &conf.TCPConfig{
			AcceptProxyProtocol: config != nil && config.EnableProxyProtocol,
			HeaderConfig:        header,
		}
	case "websocket":
		host := ""
		path := ""
		if nodeInfo != nil {
			host, path = nodeInfo.Host, nodeInfo.Path
		}
		// Allow XHTTP-style local Host/Path reuse for ws if set.
		if config != nil && config.XHTTP != nil {
			if v := strings.TrimSpace(config.XHTTP.Host); v != "" {
				host = v
			}
			if v := strings.TrimSpace(config.XHTTP.Path); v != "" {
				path = v
			}
		}
		headers := map[string]string{"Host": host}
		streamSetting.WSSettings = &conf.WebSocketConfig{
			AcceptProxyProtocol: config != nil && config.EnableProxyProtocol,
			Path:                path,
			Headers:             headers,
		}
	case "splithttp":
		xhttpSettings, err := buildXHTTPSettings(config, nodeInfo)
		if err != nil {
			return nil, err
		}
		streamSetting.SplitHTTPSettings = xhttpSettings
		// Also set XHTTPSettings alias for cores that prefer the new name.
		streamSetting.XHTTPSettings = xhttpSettings
	case "grpc":
		serviceName := ""
		if nodeInfo != nil {
			serviceName = nodeInfo.ServiceName
		}
		streamSetting.GRPCSettings = &conf.GRPCConfig{ServiceName: serviceName}
	default:
		// Other transports (kcp, hysteria, …) rely on core defaults; network already set.
	}

	// Build TLS / REALITY (local Security overrides panel).
	security := resolveSecurity(config, nodeInfo)
	switch security {
	case "reality":
		streamSetting.Security = "reality"
		if nodeInfo == nil || len(nodeInfo.RealitySettings) == 0 {
			return nil, fmt.Errorf("Reality security requires realitySettings from panel (or keep Security empty and use tls for CDN)")
		}
		realitySettings := &conf.REALITYConfig{}
		if err := json.Unmarshal(nodeInfo.RealitySettings, realitySettings); err != nil {
			return nil, fmt.Errorf("Unmarshal realitySettings failed: %s", err)
		}
		applyRealityLocalOverrides(config, realitySettings)
		streamSetting.REALITYSettings = realitySettings
	case "tls":
		tlsSettings, err := buildTLSSettings(config)
		if err != nil {
			return nil, err
		}
		streamSetting.Security = "tls"
		streamSetting.TLSSettings = tlsSettings
	case "":
		// No transport security (e.g. CDN terminates TLS, origin HTTP-only XHTTP).
	default:
		return nil, fmt.Errorf("unsupported security type: %s", security)
	}

	// PROXY protocol: useful when CDN/LB preserves client IP to origin.
	if networkType != "tcp" && networkType != "ws" && config != nil && config.EnableProxyProtocol {
		streamSetting.SocketSettings = &conf.SocketConfig{
			AcceptProxyProtocol: true,
		}
	}
	return streamSetting, nil
}

// buildAnyTLSHandlerConfig manually constructs a core.InboundHandlerConfig for
// the anytls protocol, bypassing conf.InboundDetourConfig.Build() since anytls
// is not registered in xray-core's infra/conf inboundConfigLoader.
func buildAnyTLSHandlerConfig(config *Config, nodeInfo *api.NodeInfo, tag string, streamSetting *conf.StreamConfig, sniffingConfig *conf.SniffingConfig) (*core.InboundHandlerConfig, error) {
	// H-2: AnyTLS authenticates with a bare SHA-256 password prefix that is
	// transmitted before any application-layer encryption. If the transport is
	// not TLS or REALITY, the credential is exposed in plaintext to any passive
	// observer on the path. Reject the configuration early rather than silently
	// starting an insecure listener.
	sec := strings.ToLower(streamSetting.Security)
	if sec != "tls" && sec != "reality" {
		return nil, fmt.Errorf(
			"AnyTLS requires TLS or REALITY transport security (got %q); "+
				"refusing to start an insecure listener that would expose the "+
				"authentication credential in plaintext",
			streamSetting.Security,
		)
	}

	// Build receiver settings
	receiverSettings := &proxyman.ReceiverConfig{}

	if config.ListenIP != "" {
		ipAddr := xnet.ParseAddress(config.ListenIP)
		receiverSettings.Listen = (&conf.Address{ipAddr}).Build()
	}

	receiverSettings.PortList = &xnet.PortList{
		Range: []*xnet.PortRange{
			{From: uint32(nodeInfo.Port), To: uint32(nodeInfo.Port)},
		},
	}

	// Build stream settings into protobuf
	ss, err := streamSetting.Build()
	if err != nil {
		return nil, fmt.Errorf("build stream settings failed: %s", err)
	}
	receiverSettings.StreamSettings = ss

	// Build sniffing settings into protobuf
	sniff, err := sniffingConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("build sniffing settings failed: %s", err)
	}
	receiverSettings.SniffingSettings = sniff

	// Proxy settings: PaddingScheme can be customised per-node via
	// config.yml → ControllerConfig.AnyTLSPaddingScheme. When empty,
	// NewServer falls back to padding.DefaultPaddingScheme.
	// FallbackAddr (H-1) is forwarded to NewServer so it can wire the
	// fallback dialer handler for active-probing resistance.
	proxySettings := serial.ToTypedMessage(&anytls.Config{
		PaddingScheme: []byte(config.AnyTLSPaddingScheme),
		FallbackAddr:  config.AnyTLSFallback,
	})

	return &core.InboundHandlerConfig{
		Tag:              tag,
		ReceiverSettings: serial.ToTypedMessage(receiverSettings),
		ProxySettings:    proxySettings,
	}, nil
}


func normalizeSecurityType(security string) string {
	switch strings.ToLower(security) {
	case "xtls":
		return "tls"
	case "tls", "reality":
		return strings.ToLower(security)
	default:
		return ""
	}
}

// applyRealityLocalOverrides applies ControllerConfig knobs onto REALITY settings
// built from the panel. Local non-empty values always win.
func applyRealityLocalOverrides(config *Config, realitySettings *conf.REALITYConfig) {
	if realitySettings == nil {
		return
	}
	if config == nil {
		if strings.TrimSpace(realitySettings.MinClientVer) == "" {
			realitySettings.MinClientVer = defaultRealityMinClientVer
		}
		return
	}
	// minClientVer: local override → panel → built-in default (must stay non-empty
	// for xray-core ≥ 26.7.11 so the core does not force 26.3.27).
	if v := strings.TrimSpace(config.RealityMinClientVer); v != "" {
		realitySettings.MinClientVer = v
	} else if strings.TrimSpace(realitySettings.MinClientVer) == "" {
		realitySettings.MinClientVer = defaultRealityMinClientVer
	}
	// ML-DSA-65 seed: local override only when set; otherwise keep panel value.
	if v := strings.TrimSpace(config.RealityMldsa65Seed); v != "" {
		realitySettings.Mldsa65Seed = v
	}
	// show: local flag forces on (cannot force off if panel already set true —
	// panel rarely sets show; this is the ops debug switch).
	if config.RealityShow {
		realitySettings.Show = true
	}
}

func normalizeTransportProtocol(protocol string) string {
	switch strings.ToLower(protocol) {
	case "http", "h2", "h3", "quic":
		return "xhttp"
	default:
		return protocol
	}
}

func getCertFile(certConfig *CertConfig) (certFile string, keyFile string, err error) {
	if certConfig.CertMode == "file" {
		if certConfig.CertFile == "" || certConfig.KeyFile == "" {
			return "", "", fmt.Errorf("Cert file path or key file path not exist")
		}
		return certConfig.CertFile, certConfig.KeyFile, nil
	} else if certConfig.CertMode == "dns" {
		lego, err := legocmd.New()
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.DNSCert(certConfig.CertDomain, certConfig.Email, certConfig.Provider, certConfig.DNSEnv)
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	} else if certConfig.CertMode == "http" {
		lego, err := legocmd.New()
		if err != nil {
			return "", "", err
		}
		certPath, keyPath, err := lego.HTTPCert(certConfig.CertDomain, certConfig.Email)
		if err != nil {
			return "", "", err
		}
		return certPath, keyPath, err
	}

	return "", "", fmt.Errorf("Unsupported certmode: %s", certConfig.CertMode)
}

func buildVlessFallbacks(fallbackConfigs []*FallBackConfig) ([]*conf.VLessInboundFallback, error) {
	if fallbackConfigs == nil {
		return nil, fmt.Errorf("You must provide FallBackConfigs")
	}

	vlessFallBacks := make([]*conf.VLessInboundFallback, len(fallbackConfigs))
	for i, c := range fallbackConfigs {

		if c.Dest == "" {
			return nil, fmt.Errorf("Dest is required for fallback failed")
		}

		var dest json.RawMessage
		dest, err := json.Marshal(c.Dest)
		if err != nil {
			return nil, fmt.Errorf("Marshal dest %s config failed: %s", dest, err)
		}
		vlessFallBacks[i] = &conf.VLessInboundFallback{
			Name: c.SNI,
			Alpn: c.Alpn,
			Path: c.Path,
			Dest: dest,
			Xver: c.ProxyProtocolVer,
		}
	}
	return vlessFallBacks, nil
}
