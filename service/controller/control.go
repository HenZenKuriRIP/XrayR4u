package controller

import (
	"context"
	"fmt"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/app/mydispatcher"
	"github.com/HenZenKuriRIP/XrayR4u/proxy/anytls"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/proxy"
)

func (c *Controller) removeInbound(tag string) error {
	inboundManager := c.server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	err := inboundManager.RemoveHandler(context.Background(), tag)
	return err
}

func (c *Controller) removeOutbound(tag string) error {
	outboundManager := c.server.GetFeature(outbound.ManagerType()).(outbound.Manager)
	err := outboundManager.RemoveHandler(context.Background(), tag)
	return err
}

func (c *Controller) addInbound(config *core.InboundHandlerConfig) error {
	inboundManager := c.server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	rawHandler, err := core.CreateObject(c.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(inbound.Handler)
	if !ok {
		return fmt.Errorf("not an InboundHandler: %s", err)
	}
	if err := inboundManager.AddHandler(context.Background(), handler); err != nil {
		return err
	}
	return nil
}

func (c *Controller) addOutbound(config *core.OutboundHandlerConfig) error {
	outboundManager := c.server.GetFeature(outbound.ManagerType()).(outbound.Manager)
	rawHandler, err := core.CreateObject(c.server, config)
	if err != nil {
		return err
	}
	handler, ok := rawHandler.(outbound.Handler)
	if !ok {
		return fmt.Errorf("not an OutboundHandler: %s", err)
	}
	if err := outboundManager.AddHandler(context.Background(), handler); err != nil {
		return err
	}
	return nil
}

func (c *Controller) addUsers(users []*protocol.User, tag string) error {
	inboundManager := c.server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := inboundManager.GetHandler(context.Background(), tag)
	if err != nil {
		return fmt.Errorf("No such inbound tag: %s", err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.GetInbound", tag)
	}

	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.UserManager", err)
	}
	for _, item := range users {
		mUser, err := item.ToMemoryUser()
		if err != nil {
			return err
		}
		err = userManager.AddUser(context.Background(), mUser)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) removeUsers(users []string, tag string) error {
	inboundManager := c.server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := inboundManager.GetHandler(context.Background(), tag)
	if err != nil {
		return fmt.Errorf("No such inbound tag: %s", err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.GetInbound", tag)
	}

	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("handler %s is not implement proxy.UserManager", err)
	}
	for _, email := range users {
		err = userManager.RemoveUser(context.Background(), email)
		if err != nil {
			return err
		}
	}
	return nil
}

// getTraffic atomically swaps each counter to 0 and returns the previous values.
// Using Set(0) (atomic Swap) avoids losing traffic that arrives between a separate
// Value() and Set(0) pair.
func (c *Controller) getTraffic(email string) (up int64, down int64) {
	upName := "user>>>" + email + ">>>traffic>>>uplink"
	downName := "user>>>" + email + ">>>traffic>>>downlink"
	statsManager := c.server.GetFeature(stats.ManagerType()).(stats.Manager)
	if upCounter := statsManager.GetCounter(upName); upCounter != nil {
		up = upCounter.Set(0)
	}
	if downCounter := statsManager.GetCounter(downName); downCounter != nil {
		down = downCounter.Set(0)
	}
	return up, down
}

// restoreTraffic adds previously drained traffic back onto the counters.
// Used when ReportUserTraffic fails so usage is not permanently lost.
func (c *Controller) restoreTraffic(email string, up, down int64) {
	if up == 0 && down == 0 {
		return
	}
	upName := "user>>>" + email + ">>>traffic>>>uplink"
	downName := "user>>>" + email + ">>>traffic>>>downlink"
	statsManager := c.server.GetFeature(stats.ManagerType()).(stats.Manager)
	if up > 0 {
		if upCounter := statsManager.GetCounter(upName); upCounter != nil {
			upCounter.Add(up)
		} else if c, err := statsManager.GetOrRegisterCounter(upName); err == nil && c != nil {
			c.Add(up)
		}
	}
	if down > 0 {
		if downCounter := statsManager.GetCounter(downName); downCounter != nil {
			downCounter.Add(down)
		} else if c, err := statsManager.GetOrRegisterCounter(downName); err == nil && c != nil {
			c.Add(down)
		}
	}
}

func (c *Controller) AddInboundLimiter(tag string, nodeSpeedLimit uint64, userList *[]api.UserInfo) error {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	err := dispather.Limiter.AddInboundLimiter(tag, nodeSpeedLimit, userList)
	return err
}

func (c *Controller) UpdateInboundLimiter(tag string, updatedUserList *[]api.UserInfo) error {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	err := dispather.Limiter.UpdateInboundLimiter(tag, updatedUserList)
	return err
}

func (c *Controller) DeleteInboundLimiter(tag string) error {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	err := dispather.Limiter.DeleteInboundLimiter(tag)
	return err
}

// DeleteUsersFromLimiter removes stale per-user state (userInfo / bucketHub /
// onlineIPs) for users that have been deleted from the panel. Without this,
// removed users' deviceLimit and speedLimit entries persist in the limiter's
// sync.Maps indefinitely.
func (c *Controller) DeleteUsersFromLimiter(tag string, deletedUsers []api.UserInfo) error {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	return dispather.Limiter.DeleteUsersFromLimiter(tag, deletedUsers)
}

func (c *Controller) GetOnlineDevice(tag string) (*[]api.OnlineUser, error) {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	users, err := dispather.Limiter.GetOnlineDevice(tag)
	// Maintenance prune separate from reporting so rate-limit buckets are not
	// reset as a side effect of online-user push.
	dispather.Limiter.PruneStaleEntries(tag)
	return users, err
}

func (c *Controller) UpdateRule(tag string, newRuleList []api.DetectRule) error {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	err := dispather.RuleManager.UpdateRule(tag, newRuleList)
	return err
}

func (c *Controller) GetDetectResult(tag string) (*[]api.DetectResult, error) {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	return dispather.RuleManager.GetDetectResult(tag)
}

func (c *Controller) DeleteRule(tag string) {
	dispather := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
	dispather.RuleManager.DeleteRule(tag)
}

// getAnyTLSServer retrieves the anytls Server instance from the xray-core
// inbound manager for the given tag. This is used by the controller to call
// UpdateUsers() directly, bypassing xray-core's UserManager interface.
func (c *Controller) getAnyTLSServer(tag string) (*anytls.Server, error) {
	inboundManager := c.server.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := inboundManager.GetHandler(context.Background(), tag)
	if err != nil {
		return nil, fmt.Errorf("no such inbound tag: %s: %w", tag, err)
	}
	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return nil, fmt.Errorf("handler %s does not implement proxy.GetInbound", tag)
	}
	server, ok := inboundInstance.GetInbound().(*anytls.Server)
	if !ok {
		return nil, fmt.Errorf("handler %s is not an anytls Server (got %T)", tag, inboundInstance.GetInbound())
	}
	return server, nil
}
