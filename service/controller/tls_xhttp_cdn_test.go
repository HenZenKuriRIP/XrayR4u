package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/stretchr/testify/require"
	"github.com/xtls/xray-core/infra/conf"
)

func TestResolveTransportAndSecurity(t *testing.T) {
	t.Parallel()

	t.Run("local transport wins", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Transport: "xhttp"}
		node := &api.NodeInfo{TransportProtocol: "tcp"}
		require.Equal(t, "xhttp", resolveTransport(cfg, node))
	})

	t.Run("local security wins", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Security: "tls"}
		node := &api.NodeInfo{TLSType: "reality"}
		require.Equal(t, "tls", resolveSecurity(cfg, node))
	})

	t.Run("security none", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", resolveSecurity(&Config{Security: "none"}, nil))
	})
}

func TestResolveVision_XHTTPAutoOff(t *testing.T) {
	t.Parallel()

	// Panel silent + XHTTP → off
	require.False(t, resolveVision(&Config{Transport: "xhttp"}, &api.NodeInfo{}))

	// Panel wants vision on XHTTP → on
	require.True(t, resolveVision(&Config{Transport: "xhttp"}, &api.NodeInfo{EnableVision: true}))

	// Force off even if panel on
	require.False(t, resolveVision(&Config{Transport: "xhttp", Vision: "off"}, &api.NodeInfo{EnableVision: true}))

	// Force on
	require.True(t, resolveVision(&Config{Transport: "tcp", Vision: "on"}, &api.NodeInfo{}))

	// TCP + panel vision
	require.True(t, resolveVision(&Config{}, &api.NodeInfo{EnableVision: true, TransportProtocol: "tcp"}))
}

func TestBuildStreamSetting_TLSXHTTPCDN(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	// Minimal PEM placeholders — Build() of stream only embeds paths; cert load is deferred by core.
	require.NoError(t, os.WriteFile(cert, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(key, []byte("-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n"), 0o600))

	cfg := &Config{
		Transport: "xhttp",
		Security:  "tls",
		Vision:    "off",
		XHTTP: &XHTTPConfig{
			Host: "cdn.example.com",
			Path: "/vless-cdn",
			Mode: "auto",
		},
		CertConfig: &CertConfig{
			CertMode: "file",
			CertFile: cert,
			KeyFile:  key,
			// leave ALPN/curves empty → PQ-ready defaults
		},
		// Server-side VLESS Encryption shape from `xray vlessenc`:
		//   mlkem768x25519plus.native.600s.<32-byte X25519 private | 64-byte ML-KEM seed>
		// (Client side uses 0rtt + public/client keys — not tested here.)
		VlessDecryption: "mlkem768x25519plus.native.600s." +
			"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // 32 zero bytes, base64.RawURLEncoding
	}
	// Panel can be empty / wrong — local fully drives CDN stack
	node := &api.NodeInfo{
		NodeType:          "vless",
		Port:              443,
		TransportProtocol: "tcp",
		TLSType:           "",
	}

	stream, err := buildStreamSetting(cfg, node)
	require.NoError(t, err)
	require.NotNil(t, stream.Network)
	require.Equal(t, "xhttp", string(*stream.Network))
	require.Equal(t, "tls", stream.Security)
	require.NotNil(t, stream.SplitHTTPSettings)
	require.Equal(t, "cdn.example.com", stream.SplitHTTPSettings.Host)
	require.Equal(t, "/vless-cdn", stream.SplitHTTPSettings.Path)
	require.Equal(t, "auto", stream.SplitHTTPSettings.Mode)
	require.NotNil(t, stream.TLSSettings)
	require.NotNil(t, stream.TLSSettings.ALPN)
	require.True(t, stringListHas(*stream.TLSSettings.ALPN, "h2"))
	require.NotNil(t, stream.TLSSettings.CurvePreferences)
	require.True(t, stringListHas(*stream.TLSSettings.CurvePreferences, "X25519MLKEM768"))

	// Inbound build with Encryption + no fallback
	handler, err := InboundBuilder(cfg, node, "cdn-tag")
	require.NoError(t, err)
	require.NotNil(t, handler)
}

func TestBuildXHTTPSettings_RequiresPath(t *testing.T) {
	t.Parallel()
	_, err := buildXHTTPSettings(&Config{XHTTP: &XHTTPConfig{Host: "x.com"}}, &api.NodeInfo{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Path")
}

func TestBuildXHTTPSettings_PathSlash(t *testing.T) {
	t.Parallel()
	s, err := buildXHTTPSettings(&Config{XHTTP: &XHTTPConfig{Path: "noprefix"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "/noprefix", s.Path)
}

func TestBuildXHTTPSettings_ExtraMerge(t *testing.T) {
	t.Parallel()
	s, err := buildXHTTPSettings(&Config{XHTTP: &XHTTPConfig{
		Path:  "/p",
		Host:  "h.example",
		Mode:  "stream-up",
		Extra: `{"noSSEHeader":true,"scMaxBufferedPosts":100}`,
	}}, nil)
	require.NoError(t, err)
	require.Equal(t, "/p", s.Path)
	require.Equal(t, "h.example", s.Host)
	require.Equal(t, "stream-up", s.Mode)
	require.True(t, s.NoSSEHeader)
	require.Equal(t, int64(100), s.ScMaxBufferedPosts)
}

func stringListHas(list conf.StringList, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
