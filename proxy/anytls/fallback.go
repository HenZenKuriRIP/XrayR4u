package anytls

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidateFallbackAddr checks that AnyTLSFallback is a usable host:port and is
// not an obvious cloud-metadata / SSRF target. Empty addr is valid (fallback
// disabled). Private/LAN addresses are intentionally allowed — operators often
// point fallback at local nginx (127.0.0.1:8443).
func ValidateFallbackAddr(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("AnyTLSFallback must be host:port (got %q): %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("AnyTLSFallback host is empty")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("AnyTLSFallback invalid port %q", portStr)
	}
	if isBlockedFallbackHost(host) {
		return fmt.Errorf("AnyTLSFallback target blocked (metadata/SSRF): %s", host)
	}
	// If host is already an IP literal, reject blocked ranges immediately.
	if ip := net.ParseIP(host); ip != nil && isBlockedFallbackIP(ip) {
		return fmt.Errorf("AnyTLSFallback target blocked (metadata/SSRF): %s", host)
	}
	return nil
}

// isBlockedFallbackHost matches well-known metadata hostnames (case-insensitive).
func isBlockedFallbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	switch h {
	case "metadata", "metadata.google.internal", "metadata.goog",
		"instance-data", "instance-data.ec2.internal":
		return true
	}
	// Anything under these suffixes is treated as metadata control plane.
	if strings.HasSuffix(h, ".metadata.google.internal") ||
		strings.HasSuffix(h, ".metadata.goog") {
		return true
	}
	return false
}

// isBlockedFallbackIP blocks link-local / cloud metadata addresses only.
// RFC1918 and loopback remain allowed for intentional local HTTPS fallback.
func isBlockedFallbackIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// IPv4 link-local 169.254.0.0/16 (AWS/GCP/Azure metadata is 169.254.169.254)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// Alibaba Cloud metadata
		if ip4[0] == 100 && ip4[1] == 100 && ip4[2] == 100 && ip4[3] == 200 {
			return true
		}
		return false
	}
	// IPv6: AWS IMDS fd00:ec2::254, link-local fe80::/10
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// fd00:ec2::/32 style unique-local used by AWS for IPv6 IMDS
	if len(ip) == net.IPv6len && ip[0] == 0xfd && ip[1] == 0x00 &&
		ip[2] == 0xec && ip[3] == 0x02 {
		return true
	}
	return false
}

// resolveAndCheckFallback resolves host and ensures no result is a blocked IP.
// Used at dial time so a hostname cannot DNS-rebind onto metadata endpoints.
func resolveAndCheckFallback(addr string) error {
	if err := ValidateFallbackAddr(addr); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedFallbackIP(ip) {
			return fmt.Errorf("AnyTLSFallback target blocked (metadata/SSRF): %s", host)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Leave dial to surface the real network error; do not fail open on
		// resolve failure by returning nil — require successful resolve first.
		return fmt.Errorf("AnyTLSFallback resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("AnyTLSFallback resolve %s: no addresses", host)
	}
	for _, ip := range ips {
		if isBlockedFallbackIP(ip) {
			return fmt.Errorf("AnyTLSFallback resolved to blocked address %s", ip)
		}
	}
	return nil
}
