package controller

import (
	"encoding/json"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/stretchr/testify/require"
	"github.com/xtls/xray-core/infra/conf"
)

func TestBuildStreamSetting_RealityMinClientVer(t *testing.T) {
	t.Parallel()

	panelSettings, err := json.Marshal(map[string]interface{}{
		"show":         false,
		"dest":         "www.example.com:443",
		"serverNames":  []string{"www.example.com"},
		"privateKey":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // 32-byte-looking placeholder; not built
		"shortIds":     []string{"abcd"},
		"minClientVer": "1.8.0",
	})
	require.NoError(t, err)

	node := &api.NodeInfo{
		NodeType:          "vless",
		Port:              443,
		TransportProtocol: "tcp",
		TLSType:           "reality",
		EnableTLS:         true,
		RealitySettings:   panelSettings,
	}

	t.Run("config.yml override wins", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{RealityMinClientVer: "26.3.27"}
		stream, err := buildStreamSetting(cfg, node)
		require.NoError(t, err)
		require.Equal(t, "reality", stream.Security)
		require.NotNil(t, stream.REALITYSettings)
		require.Equal(t, "26.3.27", stream.REALITYSettings.MinClientVer)
	})

	t.Run("empty config keeps panel value", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{RealityMinClientVer: ""}
		stream, err := buildStreamSetting(cfg, node)
		require.NoError(t, err)
		require.Equal(t, "1.8.0", stream.REALITYSettings.MinClientVer)
	})

	t.Run("empty panel and empty config fall back to default", func(t *testing.T) {
		t.Parallel()
		emptyPanel, err := json.Marshal(map[string]interface{}{
			"show":        false,
			"dest":        "www.example.com:443",
			"serverNames": []string{"www.example.com"},
			"privateKey":  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			"shortIds":    []string{"abcd"},
		})
		require.NoError(t, err)
		nodeEmpty := &api.NodeInfo{
			NodeType:          "vless",
			Port:              443,
			TransportProtocol: "tcp",
			TLSType:           "reality",
			EnableTLS:         true,
			RealitySettings:   emptyPanel,
		}
		cfg := &Config{}
		stream, err := buildStreamSetting(cfg, nodeEmpty)
		require.NoError(t, err)
		require.Equal(t, defaultRealityMinClientVer, stream.REALITYSettings.MinClientVer)
	})
}

func TestBuildStreamSetting_RealityMinClientVer_WhitespaceOverride(t *testing.T) {
	t.Parallel()

	panelSettings, err := json.Marshal(map[string]interface{}{
		"dest":         "www.example.com:443",
		"serverNames":  []string{"www.example.com"},
		"privateKey":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"shortIds":     []string{"ab"},
		"minClientVer": "1.8.0",
	})
	require.NoError(t, err)

	node := &api.NodeInfo{
		TransportProtocol: "tcp",
		TLSType:           "reality",
		RealitySettings:   panelSettings,
	}
	cfg := &Config{RealityMinClientVer: "  2.0.0  "}
	stream, err := buildStreamSetting(cfg, node)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", stream.REALITYSettings.MinClientVer)

	// Ensure we still produce a conf.REALITYConfig pointer type.
	var _ *conf.REALITYConfig = stream.REALITYSettings
}
