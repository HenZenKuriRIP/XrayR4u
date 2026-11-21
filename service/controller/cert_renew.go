package controller

import (
	"crypto/x509"
	"encoding/pem"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/common/legocmd"
)

const (
	certRenewHorizon = 30 * 24 * time.Hour
	certRenewMinGap  = 12 * time.Hour
)

// renewCertFn is replaced in tests. The default calls lego.
var renewCertFn = defaultRenewCert

func defaultRenewCert(domain, email, certMode, provider string, dnsEnv map[string]string) error {
	lego, err := legocmd.New()
	if err != nil {
		return err
	}
	_, _, err = lego.RenewCert(domain, email, certMode, provider, dnsEnv)
	return err
}

// certDirForTest overrides the certificate directory. Production leaves it empty.
var certDirForTest string

// certFilePath matches legocmd: $XRAY_LOCATION_CONFIG/cert/certificates/<domain>.crt.
func certFilePath(domain string) string {
	domain = strings.TrimSpace(domain)
	base := certDirForTest
	if base == "" {
		base = os.Getenv("XRAY_LOCATION_CONFIG")
	}
	if base == "" {
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "cert", "certificates", domain+".crt")
}

// certRenewDue is true when the certificate is missing, unreadable, or its
// NotAfter is within 30 days. A certificate further out than that is skipped
// and does not count as a renew attempt.
func certRenewDue(certPath string, now time.Time) bool {
	raw, err := os.ReadFile(certPath)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return true
	}
	rest := raw
	var leaf *x509.Certificate
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return true
		}
		leaf = cert
		break
	}
	if leaf == nil {
		return true
	}
	return !leaf.NotAfter.After(now.Add(certRenewHorizon))
}

// maybeRenewCert starts at most one lego renew, and not more often than
// every 12 hours. A certificate that is not yet due does not move lastRenew,
// so the cheap stat can run on every panel tick.
func (c *Controller) maybeRenewCert() {
	if c.config == nil || c.config.CertConfig == nil {
		return
	}
	mode := c.config.CertConfig.CertMode
	if mode != "http" && mode != "dns" {
		return
	}
	domain := strings.TrimSpace(c.config.CertConfig.CertDomain)
	now := time.Now()

	c.renewMu.Lock()
	if c.renewRunning {
		c.renewMu.Unlock()
		return
	}
	if !c.lastRenew.IsZero() && now.Sub(c.lastRenew) < certRenewMinGap {
		c.renewMu.Unlock()
		return
	}
	if !certRenewDue(certFilePath(domain), now) {
		c.renewMu.Unlock()
		return
	}
	c.renewRunning = true
	c.lastRenew = now
	email := c.config.CertConfig.Email
	provider := c.config.CertConfig.Provider
	dnsEnv := c.config.CertConfig.DNSEnv
	c.renewMu.Unlock()

	go func() {
		defer func() {
			c.renewMu.Lock()
			c.renewRunning = false
			c.renewMu.Unlock()
		}()
		if c.closeCtx != nil && c.closeCtx.Err() != nil {
			return
		}
		if err := renewCertFn(domain, email, mode, provider, dnsEnv); err != nil {
			log.Printf("[%s: %d] cert renew failed: %s", c.clientInfo.NodeType, c.clientInfo.NodeID, err)
		}
	}()
}
