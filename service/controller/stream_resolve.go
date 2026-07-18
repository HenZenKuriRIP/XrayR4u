package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/xtls/xray-core/infra/conf"
)

// defaultTLSPQCurves is applied when CertConfig.CurvePreferences is empty so
// TLS origins negotiate X25519MLKEM768 with capable clients (Go/xray default path).
var defaultTLSPQCurves = []string{"X25519MLKEM768", "X25519", "P256"}

// defaultTLSALPN is CDN/XHTTP friendly.
var defaultTLSALPN = []string{"h2", "http/1.1"}

// resolveTransport returns the effective transport protocol name for conf.TransportProtocol.
// Priority: ControllerConfig.Transport > panel NodeInfo.TransportProtocol > "tcp".
func resolveTransport(config *Config, nodeInfo *api.NodeInfo) string {
	if config != nil {
		if v := strings.TrimSpace(config.Transport); v != "" {
			return normalizeTransportProtocol(v)
		}
	}
	if nodeInfo != nil {
		if v := strings.TrimSpace(nodeInfo.TransportProtocol); v != "" {
			return normalizeTransportProtocol(v)
		}
	}
	return "tcp"
}

// resolveSecurity returns "tls" | "reality" | "".
// Priority: ControllerConfig.Security > panel TLSType.
func resolveSecurity(config *Config, nodeInfo *api.NodeInfo) string {
	if config != nil {
		if v := strings.TrimSpace(config.Security); v != "" {
			if strings.EqualFold(v, "none") || v == "0" {
				return ""
			}
			return normalizeSecurityType(v)
		}
	}
	if nodeInfo != nil {
		return normalizeSecurityType(nodeInfo.TLSType)
	}
	return ""
}

// resolveVision decides whether VLESS users get xtls-rprx-vision.
// Priority: ControllerConfig.Vision on/off > panel EnableVision, with XHTTP auto-off
// when Vision is empty/auto (CDN-safe default).
func resolveVision(config *Config, nodeInfo *api.NodeInfo) bool {
	vision := ""
	if config != nil {
		vision = strings.ToLower(strings.TrimSpace(config.Vision))
	}
	switch vision {
	case "on", "true", "1", "yes", "enable", "enabled":
		return true
	case "off", "false", "0", "no", "disable", "disabled":
		return false
	}

	// auto / empty
	transport := resolveTransport(config, nodeInfo)
	if isXHTTPTransport(transport) {
		// XHTTP+CDN: only enable Vision if panel explicitly asked for it.
		// (Empty panel + XHTTP → off.)
		if nodeInfo != nil && nodeInfo.EnableVision {
			return true
		}
		return false
	}
	if nodeInfo != nil {
		return nodeInfo.EnableVision
	}
	return false
}

func isXHTTPTransport(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "xhttp", "splithttp":
		return true
	default:
		return false
	}
}

// resolveXHTTPHostPathMode merges local XHTTP config over panel Host/Path.
func resolveXHTTPHostPathMode(config *Config, nodeInfo *api.NodeInfo) (host, path, mode string) {
	if nodeInfo != nil {
		host = strings.TrimSpace(nodeInfo.Host)
		path = strings.TrimSpace(nodeInfo.Path)
	}
	if config != nil && config.XHTTP != nil {
		if v := strings.TrimSpace(config.XHTTP.Host); v != "" {
			host = v
		}
		if v := strings.TrimSpace(config.XHTTP.Path); v != "" {
			path = v
		}
		if v := strings.TrimSpace(config.XHTTP.Mode); v != "" {
			mode = v
		}
	}
	if mode == "" {
		mode = "auto"
	}
	return host, path, mode
}

// buildXHTTPSettings constructs conf.SplitHTTPConfig for the inbound.
func buildXHTTPSettings(config *Config, nodeInfo *api.NodeInfo) (*conf.SplitHTTPConfig, error) {
	host, path, mode := resolveXHTTPHostPathMode(config, nodeInfo)
	if path == "" {
		return nil, fmt.Errorf("XHTTP requires a non-empty Path (set ControllerConfig.XHTTP.Path, e.g. /vless)")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	settings := &conf.SplitHTTPConfig{
		Host: host,
		Path: path,
		Mode: mode,
	}

	if config != nil && config.XHTTP != nil {
		x := config.XHTTP
		if len(x.Headers) > 0 {
			settings.Headers = x.Headers
		}
		settings.NoSSEHeader = x.NoSSEHeader
		settings.NoGRPCHeader = x.NoGRPCHeader
		if extra := strings.TrimSpace(x.Extra); extra != "" {
			// Merge advanced JSON onto settings while keeping Host/Path/Mode.
			var overlay conf.SplitHTTPConfig
			if err := json.Unmarshal([]byte(extra), &overlay); err != nil {
				return nil, fmt.Errorf("XHTTP.Extra JSON invalid: %w", err)
			}
			overlay.Host = settings.Host
			overlay.Path = settings.Path
			if settings.Mode != "" {
				overlay.Mode = settings.Mode
			}
			if settings.Headers != nil {
				overlay.Headers = settings.Headers
			}
			// Preserve explicit No* flags from struct when true; Extra can set them too.
			if x.NoSSEHeader {
				overlay.NoSSEHeader = true
			}
			if x.NoGRPCHeader {
				overlay.NoGRPCHeader = true
			}
			settings = &overlay
		}
	}

	return settings, nil
}

// buildTLSSettings builds conf.TLSConfig for origin TLS (CDN Full/Strict, or direct TLS).
func buildTLSSettings(config *Config) (*conf.TLSConfig, error) {
	if config == nil || config.CertConfig == nil {
		return nil, fmt.Errorf("TLS security requires CertConfig")
	}
	if config.CertConfig.CertMode == "" || config.CertConfig.CertMode == "none" {
		return nil, fmt.Errorf("TLS security requires CertConfig.CertMode (file/http/dns), got %q", config.CertConfig.CertMode)
	}

	certFile, keyFile, err := getCertFile(config.CertConfig)
	if err != nil {
		return nil, err
	}

	tlsSettings := &conf.TLSConfig{
		RejectUnknownSNI:        config.CertConfig.RejectUnknownSni,
		MinVersion:              config.CertConfig.MinVersion,
		MaxVersion:              config.CertConfig.MaxVersion,
		EnableSessionResumption: config.CertConfig.EnableSessionResumption,
		ECHServerKeys:           config.CertConfig.ECHServerKeys,
	}
	tlsSettings.Certs = append(tlsSettings.Certs, &conf.TLSCertConfig{
		CertFile:     certFile,
		KeyFile:      keyFile,
		OcspStapling: 3600,
	})

	alpn := config.CertConfig.ALPN
	if len(alpn) == 0 {
		alpn = append([]string{}, defaultTLSALPN...)
	}
	alpnList := conf.StringList(alpn)
	tlsSettings.ALPN = &alpnList

	curves := config.CertConfig.CurvePreferences
	if len(curves) == 0 {
		curves = append([]string{}, defaultTLSPQCurves...)
	}
	curveList := conf.StringList(curves)
	tlsSettings.CurvePreferences = &curveList

	return tlsSettings, nil
}
