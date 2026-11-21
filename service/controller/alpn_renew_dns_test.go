package controller

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/stretchr/testify/require"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/infra/conf"
	"google.golang.org/protobuf/proto"
)

func TestApplyHysteriaALPNWritesBackExistingH3(t *testing.T) {
	h2 := conf.StringList{"h2", "http/1.1"}
	tlsSettings := &conf.TLSConfig{ALPN: &h2}
	applyHysteriaALPN(tlsSettings, &Config{CertConfig: &CertConfig{ALPN: []string{"h3"}}})
	require.Equal(t, conf.StringList{"h3"}, *tlsSettings.ALPN)
}

func TestApplyHysteriaALPNEmptyBecomesH3(t *testing.T) {
	h2 := conf.StringList{"h2", "http/1.1"}
	tlsSettings := &conf.TLSConfig{ALPN: &h2}
	applyHysteriaALPN(tlsSettings, &Config{CertConfig: &CertConfig{}})
	require.Equal(t, conf.StringList{"h3"}, *tlsSettings.ALPN)
}

func TestApplyHysteriaALPNPrependsH3(t *testing.T) {
	tlsSettings := &conf.TLSConfig{}
	applyHysteriaALPN(tlsSettings, &Config{CertConfig: &CertConfig{ALPN: []string{"http/1.1"}}})
	require.Equal(t, conf.StringList{"h3", "http/1.1"}, *tlsSettings.ALPN)
}

func TestOutboundSendThroughUsesSendIP(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "203.0.113.10"} {
		ob, err := OutboundBuilder(&Config{SendIP: ip}, &api.NodeInfo{NodeType: "Vless"}, "tag")
		require.NoError(t, err)
		var sender proxyman.SenderConfig
		require.NoError(t, proto.Unmarshal(ob.SenderSettings.Value, &sender))
		require.NotNil(t, sender.Via)
		require.True(t, net.IP(sender.Via.GetIp()).Equal(net.ParseIP(ip)), "via=%v", sender.Via.GetIp())
	}
}

func TestCertRenewDue(t *testing.T) {
	dir := t.TempDir()
	certDirForTest = dir
	t.Cleanup(func() { certDirForTest = "" })
	far := certFilePath("far.example")
	soon := certFilePath("soon.example")
	writeCertPEM(t, far, time.Now().Add(90*24*time.Hour))
	writeCertPEM(t, soon, time.Now().Add(10*24*time.Hour))
	now := time.Now()
	require.False(t, certRenewDue(far, now))
	require.True(t, certRenewDue(soon, now))
	require.True(t, certRenewDue(filepath.Join(dir, "missing.crt"), now))
}

func TestMaybeRenewCertSkipsFarFromExpiry(t *testing.T) {
	certDirForTest = t.TempDir()
	t.Cleanup(func() { certDirForTest = "" })
	domain := "hy2.example"
	writeCertPEM(t, certFilePath(domain), time.Now().Add(90*24*time.Hour))

	prev := renewCertFn
	called := 0
	renewCertFn = func(string, string, string, string, map[string]string) error {
		called++
		return nil
	}
	t.Cleanup(func() { renewCertFn = prev })

	c := &Controller{config: &Config{CertConfig: &CertConfig{
		CertMode:   "http",
		CertDomain: domain,
		Email:      "a@b.c",
	}}}
	c.maybeRenewCert()
	time.Sleep(30 * time.Millisecond)
	require.Zero(t, called)
	require.True(t, c.lastRenew.IsZero())
	require.False(t, c.renewRunning)
}

func TestMaybeRenewCertSingleFlightAndGap(t *testing.T) {
	certDirForTest = t.TempDir()
	t.Cleanup(func() { certDirForTest = "" })
	prev := renewCertFn
	called := 0
	renewCertFn = func(string, string, string, string, map[string]string) error {
		called++
		return nil
	}
	t.Cleanup(func() { renewCertFn = prev })

	c := &Controller{
		clientInfo: api.ClientInfo{NodeType: "hysteria2", NodeID: 7},
		config: &Config{CertConfig: &CertConfig{
			CertMode:   "http",
			CertDomain: "missing.example",
			Email:      "a@b.c",
		}},
	}
	c.maybeRenewCert()
	time.Sleep(40 * time.Millisecond)
	c.maybeRenewCert()
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, 1, called)
}

func TestClassifyHysteriaDomain(t *testing.T) {
	local := []net.IP{net.ParseIP("10.0.0.5")}
	public := []net.IP{net.ParseIP("203.0.113.10")}
	records := []net.IP{net.ParseIP("203.0.113.10")}
	require.Empty(t, classifyHysteriaDomain("hy2.example", records, nil, local, public, nil))

	nx := &net.DNSError{Err: "no such host", Name: "hy2.example", IsNotFound: true}
	require.Contains(t, classifyHysteriaDomain("hy2.example", nil, nx, local, nil, nil), "NXDOMAIN")
	require.Contains(t, classifyHysteriaDomain("hy2.example", nil, nil, local, public, nil), "no A/AAAA")

	other := []net.IP{net.ParseIP("198.51.100.9")}
	msg := classifyHysteriaDomain("hy2.example", other, nil, local, nil, os.ErrClosed)
	require.Contains(t, msg, "198.51.100.9")
	require.Contains(t, msg, "public address lookup failed")
}

func TestRetryWait(t *testing.T) {
	require.Equal(t, 60*time.Second, retryWait(0))
	require.Equal(t, 60*time.Second, retryWait(60))
	require.Equal(t, 90*time.Second, retryWait(90))
	require.Equal(t, 5*time.Minute, retryWait(3600))
}

func TestStartFailureSummaryDropsBody(t *testing.T) {
	msg := startFailureSummary(&api.HTTPStatusError{
		Status: 429,
		Err:    errString("request https://panel.example/api/v1/server/UniProxy/config failed: status=429 body=secret-key"),
	})
	require.NotContains(t, msg, "secret-key")
	require.Contains(t, msg, "status=429")
	code, ok := api.StatusOf(&api.HTTPStatusError{Status: 429, Err: errString("x")})
	require.True(t, ok)
	require.Equal(t, 429, code)
}

type errString string

func (e errString) Error() string { return string(e) }

func writeCertPEM(t *testing.T, path string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hy2.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
}
