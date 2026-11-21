package controller

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/app/mydispatcher"
	_ "github.com/HenZenKuriRIP/XrayR4u/main/distro/all"
	"github.com/stretchr/testify/require"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/infra/conf"
)

type scriptedAPI struct {
	mu        sync.Mutex
	nodeCalls int
	info      api.ClientInfo
	node      *api.NodeInfo
	users     []api.UserInfo
}

func (s *scriptedAPI) GetNodeInfo() (*api.NodeInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeCalls++
	if s.nodeCalls == 1 {
		return nil, &api.HTTPStatusError{Status: 429, Err: errString("request https://panel.example/api/v1/server/UniProxy/config failed: status=429 body=rate-limit")}
	}
	cp := *s.node
	return &cp, nil
}

func (s *scriptedAPI) GetUserList() (*[]api.UserInfo, error) {
	users := append([]api.UserInfo(nil), s.users...)
	return &users, nil
}

func (s *scriptedAPI) ReportNodeStatus(*api.NodeStatus) error        { return nil }
func (s *scriptedAPI) ReportNodeOnlineUsers(*[]api.OnlineUser) error { return nil }
func (s *scriptedAPI) ReportUserTraffic(*[]api.UserTraffic) error    { return nil }
func (s *scriptedAPI) Describe() api.ClientInfo                      { return s.info }
func (s *scriptedAPI) GetNodeRule() (*[]api.DetectRule, error) {
	empty := []api.DetectRule{}
	return &empty, nil
}
func (s *scriptedAPI) ReportIllegal(*[]api.DetectResult) error { return nil }
func (s *scriptedAPI) Debug()                                  {}

func TestStartRetriesFailedNodeWithoutRestart(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	logConfig := (&conf.LogConfig{LogLevel: "warning"}).Build()
	dnsConfig, err := (&conf.DNSConfig{}).Build()
	require.NoError(t, err)
	routeConfig, err := (&conf.RouterConfig{}).Build()
	require.NoError(t, err)
	policyConfig, err := (&conf.PolicyConfig{Levels: map[uint32]*conf.Policy{0: {}}}).Build()
	require.NoError(t, err)
	server, err := core.New(&core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(logConfig),
			serial.ToTypedMessage(&mydispatcher.Config{}),
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(dnsConfig),
			serial.ToTypedMessage(routeConfig),
			serial.ToTypedMessage(policyConfig),
		},
	})
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { _ = server.Close() })

	apiClient := &scriptedAPI{
		info: api.ClientInfo{NodeType: "Vless", NodeID: 9},
		node: &api.NodeInfo{NodeType: "Vless", NodeID: 9, Port: port, TransportProtocol: "tcp"},
		users: []api.UserInfo{{
			UID:   1,
			Email: "user@example.com",
			UUID:  "11111111-1111-1111-1111-111111111111",
		}},
	}
	c := New(server, apiClient, &Config{
		ListenIP:                "127.0.0.1",
		SendIP:                  "0.0.0.0",
		UpdatePeriodic:          1,
		DisableGetRule:          true,
		DisableReportNodeStatus: true,
	}, "V2board")
	require.NoError(t, c.Start())
	t.Cleanup(func() { _ = c.Close() })

	tag := "Vless_127.0.0.1_" + itoa(port)
	im := server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	deadline := time.Now().Add(8 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		h, herr := im.GetHandler(context.Background(), tag)
		if herr == nil && h != nil {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, found, "inbound %s did not appear after the retry", tag)
	apiClient.mu.Lock()
	calls := apiClient.nodeCalls
	apiClient.mu.Unlock()
	require.GreaterOrEqual(t, calls, 2)
}
