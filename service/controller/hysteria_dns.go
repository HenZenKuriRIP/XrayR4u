package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
)

const hysteriaDNSInterval = time.Hour

// Injected so tests do not touch the network.
var (
	lookupDomainIPs = func(ctx context.Context, domain string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", domain)
	}
	lookupLocalIPs    = collectLocalIPs
	lookupPublicIPsFn = lookupPublicIPs
)

func collectLocalIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		out = append(out, ip)
	}
	return out, nil
}

func lookupPublicIPs(ctx context.Context) ([]net.IP, error) {
	endpoints := []string{
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
		"https://ifconfig.me/ip",
	}
	client := &http.Client{Timeout: 4 * time.Second}
	var last error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			last = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || readErr != nil {
			last = fmt.Errorf("public address lookup %s: status %d", endpoint, resp.StatusCode)
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(string(body)))
		if ip == nil {
			last = fmt.Errorf("public address lookup %s: not an IP", endpoint)
			continue
		}
		return []net.IP{ip}, nil
	}
	if last == nil {
		last = fmt.Errorf("public address lookup failed")
	}
	return nil, last
}

// classifyHysteriaDomain describes why a certificate domain is not usable.
// An empty string means at least one record matches a local or public address.
func classifyHysteriaDomain(domain string, records []net.IP, lookupErr error, local, public []net.IP, publicErr error) string {
	domain = strings.TrimSpace(domain)
	if lookupErr != nil {
		if isNXDOMAIN(lookupErr) {
			return fmt.Sprintf("Hysteria2 CertDomain %q: NXDOMAIN", domain)
		}
		return fmt.Sprintf("Hysteria2 CertDomain %q: lookup failed", domain)
	}
	if len(records) == 0 {
		return fmt.Sprintf("Hysteria2 CertDomain %q: no A/AAAA records", domain)
	}
	candidates := append([]net.IP{}, local...)
	candidates = append(candidates, public...)
	for _, rec := range records {
		if rec == nil {
			continue
		}
		for _, have := range candidates {
			if have != nil && rec.Equal(have) {
				return ""
			}
		}
	}
	shown := make([]string, 0, len(records))
	for _, rec := range records {
		if rec != nil {
			shown = append(shown, rec.String())
		}
	}
	msg := fmt.Sprintf("Hysteria2 CertDomain %q: records %s match neither a local address nor the public address", domain, strings.Join(shown, ","))
	if publicErr != nil {
		msg += " (public address lookup failed)"
	}
	return msg
}

func isNXDOMAIN(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr != nil && dnsErr.IsNotFound {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such host") || strings.Contains(msg, "nxdomain")
}

// maybeWarnHysteriaDomain resolves CertDomain at most once an hour.
// The same problem is logged once. A later different problem is logged again.
// The inbound is left in place.
func (c *Controller) maybeWarnHysteriaDomain(info *api.NodeInfo) {
	if info == nil || !api.IsHysteriaNode(info.NodeType) {
		return
	}
	if c.config == nil || c.config.CertConfig == nil {
		return
	}
	domain := strings.TrimSpace(c.config.CertConfig.CertDomain)
	if domain == "" {
		return
	}

	now := time.Now()
	c.dnsMu.Lock()
	if !c.lastDNSCheck.IsZero() && now.Sub(c.lastDNSCheck) < hysteriaDNSInterval {
		c.dnsMu.Unlock()
		return
	}
	c.lastDNSCheck = now
	prev := c.lastDNSWarn
	c.dnsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	records, lookupErr := lookupDomainIPs(ctx, domain)
	local, _ := lookupLocalIPs()
	public, publicErr := lookupPublicIPsFn(ctx)
	problem := classifyHysteriaDomain(domain, records, lookupErr, local, public, publicErr)

	c.dnsMu.Lock()
	defer c.dnsMu.Unlock()
	if problem == "" {
		c.lastDNSWarn = ""
		return
	}
	if problem == prev {
		return
	}
	c.lastDNSWarn = problem
	log.Printf("[%s: %d] %s", info.NodeType, info.NodeID, problem)
}
