package controller

import (
	"encoding/json"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/stretchr/testify/require"
)

func TestResolveVlessDecryption(t *testing.T) {
	t.Parallel()

	t.Run("defaults to none", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "none", resolveVlessDecryption(&Config{}, &api.NodeInfo{}))
		require.Equal(t, "none", resolveVlessDecryption(nil, nil))
	})

	t.Run("config overrides panel", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{VlessDecryption: "mlkem768x25519plus.native.0rtt.cfg"}
		node := &api.NodeInfo{VlessDecryption: "mlkem768x25519plus.native.0rtt.panel"}
		require.Equal(t, "mlkem768x25519plus.native.0rtt.cfg", resolveVlessDecryption(cfg, node))
	})

	t.Run("panel used when config empty", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{}
		node := &api.NodeInfo{VlessDecryption: "mlkem768x25519plus.native.0rtt.panel"}
		require.Equal(t, "mlkem768x25519plus.native.0rtt.panel", resolveVlessDecryption(cfg, node))
	})
}

func TestInboundBuilder_VlessEncryptionRejectsFallback(t *testing.T) {
	t.Parallel()

	node := &api.NodeInfo{
		NodeType:          "vless",
		Port:              443,
		TransportProtocol: "tcp",
		TLSType:           "",
		EnableTLS:         false,
		VlessDecryption:   "mlkem768x25519plus.native.0rtt.seed",
	}
	cfg := &Config{
		EnableFallback:  true,
		FallBackConfigs: []*FallBackConfig{{Dest: "80"}},
	}
	_, err := InboundBuilder(cfg, node, "test-tag")
	require.Error(t, err)
	require.Contains(t, err.Error(), "EnableFallback")
}

func TestInboundBuilder_VlessEncryptionNoneWithFallbackOK(t *testing.T) {
	t.Parallel()

	node := &api.NodeInfo{
		NodeType:          "vless",
		Port:              443,
		TransportProtocol: "tcp",
	}
	cfg := &Config{
		EnableFallback:  true,
		FallBackConfigs: []*FallBackConfig{{Dest: "80"}},
		// VlessDecryption empty → none
	}
	handler, err := InboundBuilder(cfg, node, "test-tag")
	require.NoError(t, err)
	require.NotNil(t, handler)
}

func TestBuildStreamSetting_Mldsa65AndShow(t *testing.T) {
	t.Parallel()

	panelSettings, err := json.Marshal(map[string]interface{}{
		"dest":         "www.example.com:443",
		"serverNames":  []string{"www.example.com"},
		"privateKey":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"shortIds":     []string{"ab"},
		"minClientVer": "1.8.0",
		"mldsa65Seed":  "panel-seed",
		"show":         false,
	})
	require.NoError(t, err)

	node := &api.NodeInfo{
		TransportProtocol: "tcp",
		TLSType:           "reality",
		RealitySettings:   panelSettings,
	}

	t.Run("panel mldsa kept when config empty", func(t *testing.T) {
		t.Parallel()
		stream, err := buildStreamSetting(&Config{}, node)
		require.NoError(t, err)
		require.Equal(t, "panel-seed", stream.REALITYSettings.Mldsa65Seed)
		require.False(t, stream.REALITYSettings.Show)
	})

	t.Run("config overrides mldsa and enables show", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			RealityMldsa65Seed: "local-seed",
			RealityShow:        true,
		}
		stream, err := buildStreamSetting(cfg, node)
		require.NoError(t, err)
		require.Equal(t, "local-seed", stream.REALITYSettings.Mldsa65Seed)
		require.True(t, stream.REALITYSettings.Show)
	})
}
