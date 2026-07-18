package v2board

import (
	"encoding/json"
	"testing"

	"github.com/bitly/go-simplejson"
	"github.com/stretchr/testify/require"
)

func tlsSettingsJSON(fields map[string]interface{}) *simplejson.Json {
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	j, err := simplejson.NewJson(raw)
	if err != nil {
		panic(err)
	}
	return j
}

func TestResolvePanelMinClientVer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields map[string]interface{}
		want   string
	}{
		{
			name:   "nil settings uses default",
			fields: nil,
			want:   defaultRealityMinClientVer,
		},
		{
			name:   "empty settings uses default",
			fields: map[string]interface{}{},
			want:   defaultRealityMinClientVer,
		},
		{
			name:   "snake_case wins",
			fields: map[string]interface{}{"min_client_ver": "1.0.0", "minClientVer": "9.9.9"},
			want:   "1.0.0",
		},
		{
			name:   "camelCase fallback",
			fields: map[string]interface{}{"minClientVer": "2.3.4"},
			want:   "2.3.4",
		},
		{
			name:   "whitespace only falls back to default",
			fields: map[string]interface{}{"min_client_ver": "   "},
			want:   defaultRealityMinClientVer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var j *simplejson.Json
			if tt.fields != nil {
				j = tlsSettingsJSON(tt.fields)
			}
			require.Equal(t, tt.want, resolvePanelMinClientVer(j))
		})
	}
}

func TestBuildRealitySettings_MinClientVer(t *testing.T) {
	t.Parallel()

	base := map[string]interface{}{
		"server_name": "www.example.com",
		"dest":        "www.example.com",
		"private_key": "priv",
		"public_key":  "pub",
		"short_id":    "abcd",
	}

	t.Run("default when panel omits", func(t *testing.T) {
		t.Parallel()
		raw := buildRealitySettings(tlsSettingsJSON(base))
		require.NotNil(t, raw)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, defaultRealityMinClientVer, got["minClientVer"])
	})

	t.Run("panel snake_case value is used", func(t *testing.T) {
		t.Parallel()
		fields := map[string]interface{}{}
		for k, v := range base {
			fields[k] = v
		}
		fields["min_client_ver"] = "26.3.27"
		raw := buildRealitySettings(tlsSettingsJSON(fields))
		require.NotNil(t, raw)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, "26.3.27", got["minClientVer"])
	})

	t.Run("mldsa65_seed is passed through", func(t *testing.T) {
		t.Parallel()
		fields := map[string]interface{}{}
		for k, v := range base {
			fields[k] = v
		}
		fields["mldsa65_seed"] = "seedseedseedseedseedseedseedseed"
		raw := buildRealitySettings(tlsSettingsJSON(fields))
		require.NotNil(t, raw)

		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, "seedseedseedseedseedseedseedseed", got["mldsa65Seed"])
	})

	t.Run("mldsa65 omitted when empty", func(t *testing.T) {
		t.Parallel()
		raw := buildRealitySettings(tlsSettingsJSON(base))
		var got map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &got))
		_, ok := got["mldsa65Seed"]
		require.False(t, ok)
	})
}

func TestResolvePanelMldsa65Seed(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", resolvePanelMldsa65Seed(nil))
	require.Equal(t, "abc", resolvePanelMldsa65Seed(tlsSettingsJSON(map[string]interface{}{
		"mldsa65_seed": "abc",
		"mldsa65Seed":  "def",
	})))
	require.Equal(t, "def", resolvePanelMldsa65Seed(tlsSettingsJSON(map[string]interface{}{
		"mldsa65Seed": "def",
	})))
}

func TestResolvePanelVlessDecryption(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", resolvePanelVlessDecryption(nil))
	require.Equal(t, "none", resolvePanelVlessDecryption(tlsSettingsJSON(map[string]interface{}{
		"decryption": "none",
	})))
	require.Equal(t, "mlkem768x25519plus.native.0rtt.xxx", resolvePanelVlessDecryption(tlsSettingsJSON(map[string]interface{}{
		"vless_decryption": "mlkem768x25519plus.native.0rtt.xxx",
	})))
	// decryption wins over vless_decryption
	require.Equal(t, "first", resolvePanelVlessDecryption(tlsSettingsJSON(map[string]interface{}{
		"decryption":       "first",
		"vless_decryption": "second",
	})))
}
