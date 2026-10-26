package v2board

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/bitly/go-simplejson"
	"github.com/stretchr/testify/require"
)

func TestRedactKey(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", redactKey(""))
	require.Equal(t, "****", redactKey("ab"))
	require.Equal(t, "abcd****", redactKey("abcdefgh"))
}

func TestSafeJSONForLog_RedactsSecrets(t *testing.T) {
	t.Parallel()
	raw, _ := json.Marshal(map[string]interface{}{
		"server_port": 443,
		"tls_settings": map[string]interface{}{
			"private_key":  "SUPER_SECRET_PRIV",
			"mldsa65_seed": "SUPER_SECRET_SEED",
			"public_key":   "pub-ok",
			"server_name":  "example.com",
		},
	})
	j, err := simplejson.NewJson(raw)
	require.NoError(t, err)
	out := safeJSONForLog(j)
	require.NotContains(t, out, "SUPER_SECRET_PRIV")
	require.NotContains(t, out, "SUPER_SECRET_SEED")
	require.Contains(t, out, "****")
	require.Contains(t, out, "pub-ok")
	require.Contains(t, out, "example.com")
}

func TestRedactURL_MasksTokenQuery(t *testing.T) {
	t.Parallel()
	in := "https://panel.example/api/v1/server/UniProxy/config?node_id=1&token=qwertyuiopasdfghjkl"
	out := redactURL(in)
	require.NotContains(t, out, "qwertyuiopasdfghjkl")
	require.Contains(t, out, "token=%2A%2A%2A%2A")
	require.Contains(t, out, "node_id=1")
}

func TestNew_UsesHeaderAuthNotQueryToken(t *testing.T) {
	t.Parallel()
	c := New(&api.Config{
		APIHost:  "https://panel.example",
		Key:      "test-api-key-xyz",
		NodeID:   1,
		NodeType: "anytls",
	})
	require.NotNil(t, c.client)
	// Query must not carry the API key.
	require.NotContains(t, c.client.QueryParam.Encode(), "token=")
	require.Equal(t, "1", c.client.QueryParam.Get("node_id"))
	// Headers must carry Bearer + X-Api-Key.
	auth := c.client.Header.Get("Authorization")
	require.True(t, strings.HasPrefix(auth, "Bearer "), "got %q", auth)
	require.Equal(t, "test-api-key-xyz", c.client.Header.Get("X-Api-Key"))
	require.Contains(t, auth, "test-api-key-xyz")
}

func TestParseUniProxyNodeResponse_InvalidPort(t *testing.T) {
	t.Parallel()
	c := &APIClient{NodeType: "vless", NodeID: 1}
	for _, port := range []int{0, -1, 70000} {
		raw, _ := json.Marshal(map[string]interface{}{
			"server_port": port,
			"network":     "tcp",
			"tls":         0,
		})
		j, err := simplejson.NewJson(raw)
		require.NoError(t, err)
		_, err = c.ParseUniProxyNodeResponse(j)
		require.Error(t, err, "port %d", port)
	}
}
