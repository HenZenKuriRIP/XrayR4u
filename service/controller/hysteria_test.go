package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/app/mydispatcher"
	_ "github.com/HenZenKuriRIP/XrayR4u/main/distro/all"
	"github.com/stretchr/testify/require"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy"
	hyaccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/transport/internet"
	"google.golang.org/protobuf/proto"
)

func TestBuildHysteriaUsers_UsesUUIDAndCompositeEmail(t *testing.T) {
	t.Parallel()
	users, err := buildHysteriaUsers("hy2_0.0.0.0_443", []api.UserInfo{
		{UID: 7, Email: "a@b.c", UUID: "11111111-1111-1111-1111-111111111111"},
		{UID: 8, UUID: "22222222-2222-2222-2222-222222222222"},
	})
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Equal(t, "hy2_0.0.0.0_443|a@b.c|7", users[0].Email)
	require.Equal(t, "hy2_0.0.0.0_443||8", users[1].Email)

	mem, err := users[0].ToMemoryUser()
	require.NoError(t, err)
	acc, ok := mem.Account.(*hyaccount.MemoryAccount)
	require.True(t, ok)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", acc.Auth)
}

func TestBuildHysteriaUsers_RejectsEmptyUUID(t *testing.T) {
	t.Parallel()
	_, err := buildHysteriaUsers("tag", []api.UserInfo{{UID: 1, Email: "a", UUID: "  "}})
	require.Error(t, err)
}

func TestInboundBuilder_Hysteria2IgnoresPanelTransport(t *testing.T) {
	cert, key := writeTestCert(t)
	node := &api.NodeInfo{
		NodeType:          "hysteria2",
		Port:              36712,
		TransportProtocol: "xhttp",
		EnableTLS:         true,
		TLSType:           "tls",
		Host:              "cdn.example",
		Path:              "",
	}
	cfg := &Config{
		ListenIP:  "127.0.0.1",
		Transport: "ws",
		CertConfig: &CertConfig{
			CertMode: "file",
			CertFile: cert,
			KeyFile:  key,
		},
		Hysteria2: &Hysteria2Config{ObfsPassword: "obfs-secret"},
	}
	built, err := InboundBuilder(cfg, node, "hy2_127.0.0.1_36712")
	require.NoError(t, err)
	require.Contains(t, built.ProxySettings.Type, "hysteria")
	require.NotContains(t, built.ProxySettings.Type, "vless")
	require.NotContains(t, built.ProxySettings.Type, "anytls")

	var recv proxyman.ReceiverConfig
	require.NoError(t, proto.Unmarshal(built.ReceiverSettings.Value, &recv))
	require.Equal(t, "hysteria", recv.StreamSettings.ProtocolName)
	require.Contains(t, recv.StreamSettings.SecurityType, "tls")
	require.Len(t, recv.StreamSettings.Udpmasks, 1)
	require.Contains(t, recv.StreamSettings.Udpmasks[0].Type, "salamander")
}

func TestInboundBuilder_Hysteria2RejectsRealityAndPlain(t *testing.T) {
	t.Parallel()
	cert, key := writeTestCert(t)
	base := &Config{CertConfig: &CertConfig{CertMode: "file", CertDomain: "hy2.example", CertFile: cert, KeyFile: key}}

	_, err := InboundBuilder(base, &api.NodeInfo{
		NodeType: "hysteria2", Port: 443, EnableTLS: true, TLSType: "reality",
	}, "t")
	require.Error(t, err)

	cfgNone := *base
	cfgNone.Security = "none"
	_, err = InboundBuilder(&cfgNone, &api.NodeInfo{
		NodeType: "hy2", Port: 443, EnableTLS: true, TLSType: "tls",
	}, "t")
	require.Error(t, err)

	_, err = InboundBuilder(base, &api.NodeInfo{
		NodeType: "hysteria", Port: 443, EnableTLS: false,
	}, "t")
	require.Error(t, err)

	_, err = InboundBuilder(base, &api.NodeInfo{
		NodeType: "hysteria2", Port: 443, EnableTLS: true, TLSType: "tls", ServerName: "other.example",
	}, "t")
	require.Error(t, err)
	require.Contains(t, err.Error(), "server_name")
	require.Contains(t, err.Error(), "CertDomain")
}

func TestInboundBuilder_VlessAndAnyTLSStayOnTheirProtocols(t *testing.T) {
	cert, key := writeTestCert(t)
	certCfg := &CertConfig{CertMode: "file", CertFile: cert, KeyFile: key}

	vless, err := InboundBuilder(&Config{}, &api.NodeInfo{
		NodeType: "Vless", Port: 443, TransportProtocol: "tcp",
	}, "vless_0.0.0.0_443")
	require.NoError(t, err)
	require.Contains(t, vless.ProxySettings.Type, "vless")
	var vrecv proxyman.ReceiverConfig
	require.NoError(t, proto.Unmarshal(vless.ReceiverSettings.Value, &vrecv))
	require.Equal(t, "tcp", vrecv.StreamSettings.ProtocolName)

	anytls, err := InboundBuilder(&Config{
		CertConfig:     certCfg,
		AnyTLSFallback: "127.0.0.1:8443",
	}, &api.NodeInfo{
		NodeType: "anytls", Port: 8443, EnableTLS: true, TLSType: "tls", TransportProtocol: "tcp",
	}, "anytls_0.0.0.0_8443")
	require.NoError(t, err)
	require.Contains(t, anytls.ProxySettings.Type, "anytls")
	var arecv proxyman.ReceiverConfig
	require.NoError(t, proto.Unmarshal(anytls.ReceiverSettings.Value, &arecv))
	require.Equal(t, "tcp", arecv.StreamSettings.ProtocolName)
	require.NotContains(t, anytls.ProxySettings.Type, "hysteria")
}

func TestHysteria2LocalHandshake(t *testing.T) {
	cert, key := writeTestCert(t)
	port := freeUDPPort(t)
	const password = "11111111-1111-1111-1111-111111111111"
	tag := "hysteria2_127.0.0.1_" + itoa(port)

	inbound, err := InboundBuilder(&Config{
		ListenIP: "127.0.0.1",
		CertConfig: &CertConfig{
			CertMode: "file",
			CertFile: cert,
			KeyFile:  key,
			ALPN:     []string{"h3"},
		},
	}, &api.NodeInfo{
		NodeType: "hysteria2", Port: port, EnableTLS: true, TLSType: "tls",
	}, tag)
	require.NoError(t, err)

	outbound, err := (&conf.OutboundDetourConfig{Protocol: "freedom", Tag: "direct"}).Build()
	require.NoError(t, err)
	dnsConfig, err := (&conf.DNSConfig{}).Build()
	require.NoError(t, err)
	routeConfig, err := (&conf.RouterConfig{}).Build()
	require.NoError(t, err)
	policyConfig, err := (&conf.PolicyConfig{Levels: map[uint32]*conf.Policy{0: {}}}).Build()
	require.NoError(t, err)
	logConfig := (&conf.LogConfig{LogLevel: "warning"}).Build()

	server, err := core.New(&core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(logConfig),
			serial.ToTypedMessage(&mydispatcher.Config{}),
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(policyConfig),
			serial.ToTypedMessage(dnsConfig),
			serial.ToTypedMessage(routeConfig),
		},
		Inbound:  []*core.InboundHandlerConfig{inbound},
		Outbound: []*core.OutboundHandlerConfig{outbound},
	})
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Close() })

	require.NoError(t, addHysteriaUser(server, tag, password))

	require.NoError(t, dialHysteria(t, port, password, cert))
	require.Error(t, dialHysteria(t, port, "not-the-password", cert))
}

func addHysteriaUser(server *core.Instance, tag, password string) error {
	im := server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := im.GetHandler(context.Background(), tag)
	if err != nil {
		return err
	}
	user, err := (&protocol.User{
		Email:   tag + "|user|1",
		Account: serial.ToTypedMessage(&hyaccount.Account{Auth: password}),
	}).ToMemoryUser()
	if err != nil {
		return err
	}
	return handler.(proxy.GetInbound).GetInbound().(proxy.UserManager).AddUser(context.Background(), user)
}

func dialHysteria(t *testing.T, port int, password, certFile string) error {
	t.Helper()
	pin, err := certPin(certFile)
	if err != nil {
		return err
	}
	network := conf.TransportProtocol("hysteria")
	alpn := conf.StringList{"h3"}
	stream := &conf.StreamConfig{
		Network:  &network,
		Security: "tls",
		TLSSettings: &conf.TLSConfig{
			PinnedPeerCertSha256: pin,
			ALPN:                 &alpn,
		},
		HysteriaSettings: &conf.HysteriaConfig{Version: 2, Auth: password},
	}
	built, err := stream.Build()
	if err != nil {
		return err
	}
	mem, err := internet.ToMemoryStreamConfig(built)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dest := xnet.TCPDestination(xnet.ParseAddress("127.0.0.1"), xnet.Port(port))
	conn, err := internet.Dial(ctx, dest, mem)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func certPin(certFile string) (string, error) {
	raw, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", os.ErrInvalid
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func writeTestCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certFile, keyFile
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
