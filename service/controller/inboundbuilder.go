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
		DestOverride: &conf.StringList{"http", "tls"},
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
	var (
		proxySetting interface{}
		setting      json.RawMessage
	)
	if config.EnableFallback {
		fallbackConfigs, err := buildVlessFallbacks(config.FallBackConfigs)
		if err != nil {
			return nil, err
		}
		proxySetting = &conf.VLessInboundConfig{
			Decryption: "none",
			Fallbacks:  fallbackConfigs,
		}
	} else {
		proxySetting = &conf.VLessInboundConfig{
			Decryption: "none",
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

// buildStreamSetting constructs the stream/tls/transport settings shared by
// all protocols. It is extracted from InboundBuilder so anytls can reuse it
// without going through conf.InboundDetourConfig.Build().
func buildStreamSetting(config *Config, nodeInfo *api.NodeInfo) (*conf.StreamConfig, error) {
	streamSetting := new(conf.StreamConfig)
	transportProtocolName := normalizeTransportProtocol(nodeInfo.TransportProtocol)
	transportProtocol := conf.TransportProtocol(transportProtocolName)
	networkType, err := transportProtocol.Build()
	if err != nil {
		return nil, fmt.Errorf("convert TransportProtocol failed: %s", err)
	}
	if networkType == "tcp" {
		tcpSetting := &conf.TCPConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
			HeaderConfig:        nodeInfo.Header,
		}
		streamSetting.TCPSettings = tcpSetting
	} else if networkType == "websocket" {
		headers := make(map[string]string)
		headers["Host"] = nodeInfo.Host
		wsSettings := &conf.WebSocketConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
			Path:                nodeInfo.Path,
			Headers:             headers,
		}
		streamSetting.WSSettings = wsSettings
	} else if networkType == "splithttp" {
		splitHTTPSettings := &conf.SplitHTTPConfig{
			Host: nodeInfo.Host,
			Path: nodeInfo.Path,
		}
		streamSetting.SplitHTTPSettings = splitHTTPSettings
	} else if networkType == "grpc" {
		grpcSettings := &conf.GRPCConfig{
			ServiceName: nodeInfo.ServiceName,
		}
		streamSetting.GRPCSettings = grpcSettings
	}

	streamSetting.Network = &transportProtocol
	// Build TLS and REALITY settings
	security := normalizeSecurityType(nodeInfo.TLSType)
	switch security {
	case "reality":
		streamSetting.Security = "reality"
		if len(nodeInfo.RealitySettings) == 0 {
			return nil, fmt.Errorf("Reality security requires realitySettings")
		}
		realitySettings := &conf.REALITYConfig{}
		if err := json.Unmarshal(nodeInfo.RealitySettings, realitySettings); err != nil {
			return nil, fmt.Errorf("Unmarshal realitySettings failed: %s", err)
		}
		streamSetting.REALITYSettings = realitySettings
	case "tls":
		if !nodeInfo.EnableTLS || config.CertConfig == nil || config.CertConfig.CertMode == "none" {
			break
		}
		streamSetting.Security = "tls"
		certFile, keyFile, err := getCertFile(config.CertConfig)
		if err != nil {
			return nil, err
		}
		tlsSettings := &conf.TLSConfig{
			RejectUnknownSNI: config.CertConfig.RejectUnknownSni,
		}
		tlsSettings.Certs = append(tlsSettings.Certs, &conf.TLSCertConfig{CertFile: certFile, KeyFile: keyFile, OcspStapling: 3600})
		streamSetting.TLSSettings = tlsSettings
	case "":
	default:
		return nil, fmt.Errorf("unsupported security type: %s", nodeInfo.TLSType)
	}
	// Support ProxyProtocol for any transport protocol
	if networkType != "tcp" && networkType != "ws" && config.EnableProxyProtocol {
		sockoptConfig := &conf.SocketConfig{
			AcceptProxyProtocol: config.EnableProxyProtocol,
		}
		streamSetting.SocketSettings = sockoptConfig
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
