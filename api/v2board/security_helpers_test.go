package v2board

import (
	"encoding/json"
	"testing"

	"github.com/bitly/go-simplejson"
	"github.com/stretchr/testify/require"
)

func TestRedactKey(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", redactKey(""))
	require.Equal(t, "****", redactKey("ab"))
	require.Equal(t, "abcd****", redactKey("abcdefgh"))
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
