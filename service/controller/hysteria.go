package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	hyaccount "github.com/xtls/xray-core/proxy/hysteria/account"
)

// buildHysteriaHandlerConfig builds a Hysteria2 inbound.
// Users are not embedded here; the controller adds them through proxy.UserManager
// with the same tag|email|uid key VLESS already uses.
func buildHysteriaHandlerConfig(config *Config, nodeInfo *api.NodeInfo, tag string, sniffingConfig *conf.SniffingConfig) (*core.InboundHandlerConfig, error) {
	if nodeInfo == nil {
		return nil, fmt.Errorf("Hysteria2 requires node info")
	}
	if nodeInfo.Port <= 0 || nodeInfo.Port > 65535 {
		return nil, fmt.Errorf("Hysteria2 requires a listen port between 1 and 65535, got %d", nodeInfo.Port)
	}

	streamSetting, err := buildHysteriaStreamSetting(config, nodeInfo)
	if err != nil {
		return nil, err
	}

	inbound := &conf.InboundDetourConfig{
		Protocol:      "hysteria",
		Tag:           tag,
		StreamSetting: streamSetting,
	}
	if config != nil && config.ListenIP != "" {
		inbound.ListenOn = &conf.Address{Address: xnet.ParseAddress(config.ListenIP)}
	}
	inbound.PortList = &conf.PortList{
		Range: []conf.PortRange{{From: uint32(nodeInfo.Port), To: uint32(nodeInfo.Port)}},
	}
	if sniffingConfig != nil {
		inbound.SniffingConfig = sniffingConfig
	}

	// version 2 is required by xray-core. Users are pushed after the inbound starts.
	setting, err := json.Marshal(&conf.HysteriaServerConfig{Version: 2})
	if err != nil {
		return nil, fmt.Errorf("marshal hysteria2 settings: %w", err)
	}
	raw := json.RawMessage(setting)
	inbound.Settings = &raw
	return inbound.Build()
}

// buildHysteriaStreamSetting forces the hysteria transport and TLS.
// It does not call resolveTransport, so a copied Transport: xhttp (or the
// panel network field) cannot retarget this inbound onto the VLESS CDN path.
func buildHysteriaStreamSetting(config *Config, nodeInfo *api.NodeInfo) (*conf.StreamConfig, error) {
	if err := requireHysteriaTLS(config, nodeInfo); err != nil {
		return nil, err
	}
	if err := matchHysteriaCertDomain(config, nodeInfo); err != nil {
		return nil, err
	}
	tlsSettings, err := buildTLSSettings(config)
	if err != nil {
		return nil, err
	}
	applyHysteriaALPN(tlsSettings, config)

	network := conf.TransportProtocol("hysteria")
	stream := &conf.StreamConfig{
		Network:     &network,
		Security:    "tls",
		TLSSettings: tlsSettings,
		HysteriaSettings: &conf.HysteriaConfig{
			Version: 2,
		},
	}
	if config != nil && config.Hysteria2 != nil {
		h := config.Hysteria2
		stream.HysteriaSettings.UdpIdleTimeout = h.UdpIdleTimeout
		stream.HysteriaSettings.Masquerade = conf.Masquerade{
			Type:        h.MasqType,
			Dir:         h.MasqDir,
			Url:         h.MasqURL,
			RewriteHost: h.MasqRewriteHost,
			Insecure:    h.MasqInsecure,
			Content:     h.MasqContent,
			StatusCode:  h.MasqStatusCode,
		}
		mask, err := buildHysteriaFinalMask(h)
		if err != nil {
			return nil, err
		}
		stream.FinalMask = mask
	}
	return stream, nil
}

func requireHysteriaTLS(config *Config, nodeInfo *api.NodeInfo) error {
	if config != nil {
		switch strings.ToLower(strings.TrimSpace(config.Security)) {
		case "reality":
			return fmt.Errorf("Hysteria2 does not support REALITY; set ControllerConfig.Security to tls and panel tls=1")
		case "none", "0":
			return fmt.Errorf("Hysteria2 requires TLS; ControllerConfig.Security cannot be %q", config.Security)
		case "tls", "xtls":
			return nil
		case "":
			// Fall through to the panel tls field.
		default:
			return fmt.Errorf("Hysteria2 only supports TLS security, got %q", config.Security)
		}
	}
	if nodeInfo != nil && strings.EqualFold(nodeInfo.TLSType, "reality") {
		return fmt.Errorf("Hysteria2 does not support REALITY (panel tls=2); create the node with tls=1 and leave EnableReality off")
	}
	if nodeInfo == nil || !nodeInfo.EnableTLS || !strings.EqualFold(nodeInfo.TLSType, "tls") {
		return fmt.Errorf("Hysteria2 requires panel tls=1 and CertConfig")
	}
	return nil
}

// matchHysteriaCertDomain refuses to start when the panel SNI and the local
// certificate domain are both set and differ. The client verifies the cert
// against server_name; the inbound would otherwise come up and then fail
// that check. Empty panel SNI is left to the local CertDomain.
func matchHysteriaCertDomain(config *Config, nodeInfo *api.NodeInfo) error {
	if config == nil || config.CertConfig == nil || nodeInfo == nil {
		return nil
	}
	panelName := strings.TrimSpace(nodeInfo.ServerName)
	certName := strings.TrimSpace(config.CertConfig.CertDomain)
	if panelName == "" || certName == "" || strings.EqualFold(panelName, certName) {
		return nil
	}
	return fmt.Errorf("Hysteria2 panel server_name %q does not match CertDomain %q", panelName, certName)
}

// applyHysteriaALPN makes sure the QUIC listener offers h3.
// xray-core only fills ALPN h3 when the TLS config's NextProtos list is empty,
// and buildTLSSettings otherwise defaults to h2/http1.1 for CDN. That default
// must stay for VLESS/AnyTLS, so Hysteria2 overrides it here.
func applyHysteriaALPN(tlsSettings *conf.TLSConfig, config *Config) {
	if tlsSettings == nil {
		return
	}
	explicit := []string(nil)
	if config != nil && config.CertConfig != nil {
		explicit = config.CertConfig.ALPN
	}
	if len(explicit) == 0 {
		list := conf.StringList{"h3"}
		tlsSettings.ALPN = &list
		return
	}
	for _, a := range explicit {
		if strings.EqualFold(strings.TrimSpace(a), "h3") {
			// Write the list back. buildTLSSettings may already have copied it,
			// but a later change there must not leave h2/http1.1 in place.
			list := conf.StringList(append([]string{}, explicit...))
			tlsSettings.ALPN = &list
			return
		}
	}
	merged := conf.StringList(append([]string{"h3"}, explicit...))
	tlsSettings.ALPN = &merged
}

func buildHysteriaFinalMask(h *Hysteria2Config) (*conf.FinalMask, error) {
	if h == nil {
		return nil, nil
	}
	var mask conf.FinalMask
	used := false
	if pwd := strings.TrimSpace(h.ObfsPassword); pwd != "" {
		settings, err := json.Marshal(map[string]string{"password": pwd})
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage(settings)
		mask.Udp = []conf.Mask{{Type: "salamander", Settings: &raw}}
		used = true
	}
	congestion := strings.TrimSpace(h.Congestion)
	up := strings.TrimSpace(h.Up)
	down := strings.TrimSpace(h.Down)
	if congestion != "" || up != "" || down != "" {
		mask.QuicParams = &conf.QuicParamsConfig{
			Congestion: congestion,
			BrutalUp:   conf.Bandwidth(up),
			BrutalDown: conf.Bandwidth(down),
		}
		used = true
	}
	if !used {
		return nil, nil
	}
	return &mask, nil
}

// buildHysteriaUsers maps panel users onto Hysteria2 accounts.
// Auth is the panel UUID, which is what a hysteria2://<uuid>@host URI sends
// in the Hysteria-Auth header. Email keeps the VLESS composite key so the
// limiter, online IP map, and traffic counters do not need a new scheme.
func buildHysteriaUsers(tag string, userInfo []api.UserInfo) ([]*protocol.User, error) {
	users := make([]*protocol.User, 0, len(userInfo))
	for i := range userInfo {
		user := userInfo[i]
		auth := strings.TrimSpace(user.UUID)
		if auth == "" {
			return nil, fmt.Errorf("hysteria2 user uid=%d has an empty uuid", user.UID)
		}
		// Email stays as the panel sent it, same as buildUserTag. An empty
		// UniProxy email is already filled with the UUID in ParseUniProxyUserResponse.
		users = append(users, &protocol.User{
			Level:   0,
			Email:   fmt.Sprintf("%s|%s|%d", tag, user.Email, user.UID),
			Account: serial.ToTypedMessage(&hyaccount.Account{Auth: auth}),
		})
	}
	return users, nil
}
