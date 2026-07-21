package legocmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCertInputs(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateCertInputs("example.com", "a@b.co", "cloudflare", true))
	require.Error(t, validateCertInputs("evil.com --server=x", "a@b.co", "cloudflare", true))
	require.Error(t, validateCertInputs("example.com", "not-an-email", "cloudflare", true))
	require.Error(t, validateCertInputs("example.com", "a@b.co", "bad provider", true))
	require.NoError(t, validateCertInputs("example.com", "a@b.co", "", false))
}

func TestAllowedDNSEnvKey(t *testing.T) {
	t.Parallel()
	require.True(t, allowedDNSEnvKey("CLOUDFLARE_DNS_API_TOKEN"))
	require.True(t, allowedDNSEnvKey("ALICLOUD_ACCESS_KEY"))
	require.True(t, allowedDNSEnvKey("LEGO_CA_SERVER"))
	require.False(t, allowedDNSEnvKey("PATH"))
	require.False(t, allowedDNSEnvKey("LD_PRELOAD"))
	require.False(t, allowedDNSEnvKey("HTTP_PROXY"))
	require.False(t, allowedDNSEnvKey("key=value"))
}

func TestApplyDNSEnvCleanup(t *testing.T) {
	key := "XRAYR_TEST_DNS_ENV_ONLY"
	_ = os.Unsetenv(key)
	cleanup, err := applyDNSEnv(map[string]string{key: "secret"})
	require.NoError(t, err)
	require.Equal(t, "secret", os.Getenv(key))
	cleanup()
	_, ok := os.LookupEnv(key)
	require.False(t, ok)

	// Restore a previous value.
	require.NoError(t, os.Setenv(key, "old"))
	cleanup, err = applyDNSEnv(map[string]string{key: "new"})
	require.NoError(t, err)
	require.Equal(t, "new", os.Getenv(key))
	cleanup()
	require.Equal(t, "old", os.Getenv(key))
	_ = os.Unsetenv(key)

	_, err = applyDNSEnv(map[string]string{"PATH": "/evil"})
	require.Error(t, err)
}

func TestRedactNotInThisPackage(t *testing.T) {
	// Placeholder to keep package tests focused.
	t.Parallel()
}
