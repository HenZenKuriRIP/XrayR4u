package anytls

import (
	"net"
	"testing"
)

func TestValidateFallbackAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		addr    string
		wantErr bool
	}{
		{"", false},
		{"127.0.0.1:8443", false},
		{"localhost:443", false},
		{"example.com:443", false},
		{"[::1]:8443", false},
		{"10.0.0.5:443", false}, // intentional LAN fallback allowed
		{"no-port", true},
		{":443", true},
		{"127.0.0.1:0", true},
		{"127.0.0.1:70000", true},
		{"169.254.169.254:80", true},
		{"metadata.google.internal:80", true},
		{"METADATA.GOOGLE.INTERNAL:80", true},
		{"100.100.100.200:80", true},
	}
	for _, tc := range cases {
		err := ValidateFallbackAddr(tc.addr)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateFallbackAddr(%q) expected error", tc.addr)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateFallbackAddr(%q) unexpected error: %v", tc.addr, err)
		}
	}
}

func TestIsBlockedFallbackIP(t *testing.T) {
	t.Parallel()
	blocked := []string{"169.254.169.254", "169.254.0.1", "100.100.100.200"}
	allowed := []string{"127.0.0.1", "10.0.0.1", "8.8.8.8", "::1", "2001:db8::1"}
	for _, s := range blocked {
		if !isBlockedFallbackIP(net.ParseIP(s)) {
			t.Errorf("expected blocked: %s", s)
		}
	}
	for _, s := range allowed {
		if isBlockedFallbackIP(net.ParseIP(s)) {
			t.Errorf("expected allowed: %s", s)
		}
	}
}
