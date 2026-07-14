package controller

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/app/mydispatcher"
	"github.com/HenZenKuriRIP/XrayR4u/common/legocmd"
	"github.com/HenZenKuriRIP/XrayR4u/common/serverstatus"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
)

// nodeState bundles nodeInfo + Tag so they are always swapped atomically.
// Readers take an RLock and copy the pointer; the struct itself is immutable
// once stored.
type nodeState struct {
	nodeInfo *api.NodeInfo
	tag      string
}

type Controller struct {
	// ---- immutable after construction (set in New, never written again) ----
	// Safe to read from any goroutine without a lock.
	server    *core.Instance
	config    *Config
	apiClient api.API

	// ---- immutable after Start() returns (written once before any goroutine
	// that reads them is launched, establishing a happens-before edge per the
	// Go memory model §"Goroutine creation") ----
	// Safe to read from any goroutine spawned inside or after Start().
	panelType  string
	clientInfo api.ClientInfo

	// ---- mutable state: all reads/writes must be done under mu ----
	// mu guards state and userList.
	mu       sync.RWMutex
	state    nodeState      // immutable snapshot, replaced atomically under mu
	userList []api.UserInfo // owned copy, never shared with callers; read via mu.RLock

	// closeCtx is cancelled in Close() before closing periodic tasks,
	// so in-flight Execute goroutines can bail out early.
	closeCtx    context.Context
	closeCancel context.CancelFunc

	nodeInfoMonitorPeriodic *task.Periodic
	userReportPeriodic      *task.Periodic
}

// New return a Controller service with default parameters.
func New(server *core.Instance, api api.API, config *Config, panelType string) *Controller {
	return &Controller{
		server:    server,
		config:    config,
		apiClient: api,
		panelType: panelType,
	}
}

// ---- thread-safe state accessors ----------------------------------------

func (c *Controller) getState() nodeState {
	c.mu.RLock()
	s := c.state
	c.mu.RUnlock()
	return s
}

// setState atomically replaces nodeInfo + tag together.
func (c *Controller) setState(info *api.NodeInfo, tag string) {
	c.mu.Lock()
	c.state = nodeState{nodeInfo: info, tag: tag}
	c.mu.Unlock()
}

// getUserList returns a snapshot of the current user list.
// The returned slice must NOT be modified by the caller.
func (c *Controller) getUserList() []api.UserInfo {
	c.mu.RLock()
	ul := c.userList
	c.mu.RUnlock()
	return ul
}

func (c *Controller) setUserList(list []api.UserInfo) {
	c.mu.Lock()
	c.userList = list
	c.mu.Unlock()
}

// -------------------------------------------------------------------------

// Start implement the Start() function of the service interface
func (c *Controller) Start() error {
	c.clientInfo = c.apiClient.Describe()

	// First fetch Node Info
	newNodeInfo, err := c.apiClient.GetNodeInfo()
	if err != nil {
		return err
	}

	// Bootstrap state before starting any goroutines – no lock needed yet.
	c.closeCtx, c.closeCancel = context.WithCancel(context.Background())
	tag := buildNodeTag(newNodeInfo, c.config.ListenIP)
	c.state = nodeState{nodeInfo: newNodeInfo, tag: tag}

	// rollbackInbound tears down a partially-started controller so a failed
	// Start() never leaves orphan inbound/outbound/limiter/rule entries.
	rollbackInbound := func() {
		_ = c.removeOldTag(tag)
		_ = c.DeleteInboundLimiter(tag)
		c.DeleteRule(tag)
	}

	// Add new tag
	if err = c.addNewTag(newNodeInfo); err != nil {
		log.Printf("[%s: %d] FATAL: Failed to add inbound tag: %s", c.clientInfo.NodeType, c.clientInfo.NodeID, err)
		return err
	}

	// Update user
	userInfo, err := c.apiClient.GetUserList()
	if err != nil {
		rollbackInbound()
		return err
	}
	if err = c.addNewUser(userInfo, newNodeInfo); err != nil {
		rollbackInbound()
		return err
	}
	c.userList = *userInfo

	// Add Limiter
	if err := c.AddInboundLimiter(tag, newNodeInfo.SpeedLimit, userInfo); err != nil {
		log.Print(err)
	}

	// Add Rule Manager
	if !c.config.DisableGetRule {
		if ruleList, err := c.apiClient.GetNodeRule(); err != nil {
			log.Printf("Get rule list failed: %s", err)
		} else if len(*ruleList) > 0 {
			if err := c.UpdateRule(tag, *ruleList); err != nil {
				log.Print(err)
			}
		}
	}

	c.nodeInfoMonitorPeriodic = &task.Periodic{
		Interval: time.Duration(c.config.UpdatePeriodic) * time.Second,
		Execute:  c.nodeInfoMonitor,
	}
	c.userReportPeriodic = &task.Periodic{
		Interval: time.Duration(c.config.UpdatePeriodic) * time.Second,
		Execute:  c.userInfoMonitor,
	}

	log.Printf("[%s: %d] Start monitor node status", newNodeInfo.NodeType, newNodeInfo.NodeID)
	go func() {
		time.Sleep(time.Duration(c.config.UpdatePeriodic) * time.Second)
		_ = c.nodeInfoMonitorPeriodic.Start()
	}()

	log.Printf("[%s: %d] Start report node status", newNodeInfo.NodeType, newNodeInfo.NodeID)
	go func() {
		time.Sleep(time.Duration(c.config.UpdatePeriodic) * time.Second)
		_ = c.userReportPeriodic.Start()
	}()
	return nil
}

// Close implement the Close() function of the service interface
func (c *Controller) Close() error {
	// Cancel the close context first so any in-flight Execute goroutines
	// on the periodic tasks see the cancellation and bail out early.
	if c.closeCancel != nil {
		c.closeCancel()
	}

	// Clean up limiter and rule-manager entries for this tag.
	// Without this, sync.Map entries accumulate across hot-reloads.
	tag := c.getState().tag
	_ = c.DeleteInboundLimiter(tag)
	c.DeleteRule(tag)

	if c.nodeInfoMonitorPeriodic != nil {
		if err := c.nodeInfoMonitorPeriodic.Close(); err != nil {
			log.Printf("node info periodic close failed: %s", err)
		}
	}
	if c.userReportPeriodic != nil {
		if err := c.userReportPeriodic.Close(); err != nil {
			log.Printf("user report periodic close failed: %s", err)
		}
	}
	return nil
}

func (c *Controller) nodeInfoMonitor() error {
	// If Close() has been called, bail out early.
	if c.closeCtx.Err() != nil {
		return nil
	}

	// Fetch remote data outside the lock – these are pure network calls.
	newNodeInfo, err := c.apiClient.GetNodeInfo()
	if err != nil {
		log.Print(err)
		return nil
	}
	newUserInfo, err := c.apiClient.GetUserList()
	if err != nil {
		log.Print(err)
		return nil
	}

	// Take a snapshot of current state for comparison – cheap RLock.
	cur := c.getState()

	nodeInfoChanged := !nodeInfoEqual(cur.nodeInfo, newNodeInfo)

	if nodeInfoChanged {
		if err := c.applyNodeInfoChange(cur, newNodeInfo, newUserInfo); err != nil {
			log.Print(err)
			return nil
		}
	}

	// Check Rule – read Tag under lock.
	currentTag := c.getState().tag
	if !c.config.DisableGetRule {
		if ruleList, err := c.apiClient.GetNodeRule(); err != nil {
			log.Printf("Get rule list failed: %s", err)
		} else if len(*ruleList) > 0 {
			if err := c.UpdateRule(currentTag, *ruleList); err != nil {
				log.Print(err)
			}
		}
	}

	// Check Cert – fire asynchronously so heavy network I/O cannot block this ticker.
	if c.getState().nodeInfo.EnableTLS &&
		(c.config.CertConfig.CertMode == "dns" || c.config.CertConfig.CertMode == "http") {
		go func() {
			lego, err := legocmd.New()
			if err != nil {
				log.Print(err)
				return
			}
			// xray-core supports OcspStapling hot renew.
			if _, _, err = lego.RenewCert(
				c.config.CertConfig.CertDomain,
				c.config.CertConfig.Email,
				c.config.CertConfig.CertMode,
				c.config.CertConfig.Provider,
				c.config.CertConfig.DNSEnv,
			); err != nil {
				log.Print(err)
			}
		}()
	}

	if !nodeInfoChanged {
		// AnyTLS: full replace via Service.UpdateUsers() — naturally
		// idempotent, no diff needed. Also refreshes the limiter.
		if strings.EqualFold(newNodeInfo.NodeType, "anytls") {
			server, err := c.getAnyTLSServer(currentTag)
			if err != nil {
				log.Print(err)
			} else {
				// Diff old vs new so we can purge limiter state for
				// users that no longer exist (or whose email key changed).
				// Delete BEFORE UpdateInboundLimiter: when only UUID changes
				// the composite key is unchanged, so deleting after update
				// would wipe the freshly written speed/device limits.
				oldList := c.getUserList()
				deleted, _ := compareUserList(oldList, *newUserInfo)

				if len(deleted) > 0 {
					if err := c.DeleteUsersFromLimiter(currentTag, deleted); err != nil {
						log.Print(err)
					}
				}
				server.UpdateUsers(c.buildAnyTLSUser(newUserInfo))
				if err := c.UpdateInboundLimiter(currentTag, newUserInfo); err != nil {
					log.Print(err)
				}
				s := c.getState()
				log.Printf("[%s: %d] anytls: full user refresh (%d users, %d deleted)",
					s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(*newUserInfo), len(deleted))
			}
		} else {
			// VLESS: incremental add/delete via UserManager
			// Diff against current snapshot – read under lock.
			oldList := c.getUserList()
			deleted, added := compareUserList(oldList, *newUserInfo)

			if len(deleted) > 0 {
				deletedEmail := make([]string, len(deleted))
				for i, u := range deleted {
					deletedEmail[i] = fmt.Sprintf("%s|%s|%d", currentTag, u.Email, u.UID)
				}
				if err := c.removeUsers(deletedEmail, currentTag); err != nil {
					log.Print(err)
				}
				// Clean up limiter state for removed users so their
				// deviceLimit / speedLimit entries don't linger.
				if err := c.DeleteUsersFromLimiter(currentTag, deleted); err != nil {
					log.Print(err)
				}
			}
			if len(added) > 0 {
				if err = c.addNewUser(&added, newNodeInfo); err != nil {
					log.Print(err)
				}
				if err := c.UpdateInboundLimiter(currentTag, &added); err != nil {
					log.Print(err)
				}
			}
			// Always refresh limiter with full user list so that changes to
			// speed_limit / device_limit for existing users take effect without
			// requiring a node-level config change or service restart.
			if len(*newUserInfo) > 0 {
				if err := c.UpdateInboundLimiter(currentTag, newUserInfo); err != nil {
					log.Print(err)
				}
			}

			s := c.getState()
			log.Printf("[%s: %d] %d user deleted, %d user added",
				s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(deleted), len(added))
		}
	}

	// Atomically publish new userList.
	c.setUserList(*newUserInfo)
	return nil
}

func (c *Controller) removeOldTag(oldtag string) error {
	if err := c.removeInbound(oldtag); err != nil {
		return err
	}
	return c.removeOutbound(oldtag)
}

func (c *Controller) addNewTag(newNodeInfo *api.NodeInfo) error {
	tag := c.getState().tag
	inboundConfig, err := InboundBuilder(c.config, newNodeInfo, tag)
	if err != nil {
		return err
	}
	if err = c.addInbound(inboundConfig); err != nil {
		return err
	}
	outBoundConfig, err := OutboundBuilder(c.config, newNodeInfo, tag)
	if err != nil {
		return err
	}
	return c.addOutbound(outBoundConfig)
}


func (c *Controller) addNewUser(userInfo *[]api.UserInfo, nodeInfo *api.NodeInfo) error {
	tag := c.getState().tag

	// AnyTLS uses the sing-anytls Service.UpdateUsers() API (full replace)
	// instead of xray-core's proxy.UserManager.AddUser() (incremental).
	if strings.EqualFold(nodeInfo.NodeType, "anytls") {
		server, err := c.getAnyTLSServer(tag)
		if err != nil {
			return err
		}
		server.UpdateUsers(c.buildAnyTLSUser(userInfo))
		s := c.getState()
		log.Printf("[%s: %d] Updated %d anytls users", s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(*userInfo))
		return nil
	}

	// VLESS: existing incremental user addition
	users := c.buildVlessUser(userInfo, nodeInfo.EnableVision)
	if err := c.addUsers(users, tag); err != nil {
		return err
	}

	s := c.getState()
	log.Printf("[%s: %d] Added %d new users", s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(*userInfo))
	return nil
}

// compareUserList returns (deleted, added) given old and new snapshots.
// UID is the primary key. When UID still exists but UUID or Email changed,
// the user is treated as delete+add so VLESS credentials and limiter/stat
// keys (tag|email|uid) are refreshed. Speed/device limit-only changes are
// handled separately via UpdateInboundLimiter.
func compareUserList(old, new []api.UserInfo) (deleted, added []api.UserInfo) {
	oldMap := make(map[int]api.UserInfo, len(old))
	for _, u := range old {
		oldMap[u.UID] = u
	}
	newMap := make(map[int]api.UserInfo, len(new))
	for _, u := range new {
		newMap[u.UID] = u
	}
	for uid, u := range oldMap {
		nu, exists := newMap[uid]
		if !exists {
			deleted = append(deleted, u)
			continue
		}
		// Credential or identity-key change: remove old, add new.
		if u.UUID != nu.UUID || u.Email != nu.Email {
			deleted = append(deleted, u)
			added = append(added, nu)
		}
	}
	for uid, u := range newMap {
		if _, exists := oldMap[uid]; !exists {
			added = append(added, u)
		}
	}
	return
}

// applyNodeInfoChange replaces the inbound/outbound and seeds users for a node
// configuration change. The entire operation is transactional: on any failure
// the previous inbound + users + limiter are restored, so there is never a
// window where the inbound exists but has no users.
//
// Strategy:
//   - Different tags (port/type changed): add new inbound → add users → remove
//     old. On failure before removeOldTag, roll back the new inbound so the old
//     one keeps serving.
//   - Same tag: must remove then add. On add inbound or add users failure,
//     restore the previous inbound with its original user set.
func (c *Controller) applyNodeInfoChange(cur nodeState, newNodeInfo *api.NodeInfo, newUserInfo *[]api.UserInfo) error {
	oldTag := cur.tag
	oldUsers := c.getUserList()
	// Copy the user slice so restore cannot observe concurrent setUserList.
	if oldUsers != nil {
		oldUsers = append([]api.UserInfo(nil), oldUsers...)
	}
	newTag := buildNodeTag(newNodeInfo, c.config.ListenIP)

	if newTag != oldTag {
		// Publish new state so addNewTag / builders use the new tag.
		c.setState(newNodeInfo, newTag)
		if err := c.addNewTag(newNodeInfo); err != nil {
			c.setState(cur.nodeInfo, oldTag)
			return fmt.Errorf("add new inbound %s failed (kept old %s): %w", newTag, oldTag, err)
		}
		// Seed users on the new inbound BEFORE tearing down the old one.
		// If this fails the old inbound is still intact — just roll back.
		if err := c.addNewUser(newUserInfo, newNodeInfo); err != nil {
			_ = c.removeOldTag(newTag)
			c.setState(cur.nodeInfo, oldTag)
			return fmt.Errorf("add users to new inbound %s failed (kept old %s): %w", newTag, oldTag, err)
		}
		if err := c.AddInboundLimiter(newTag, newNodeInfo.SpeedLimit, newUserInfo); err != nil {
			log.Print(err) // non-fatal: node works without rate limiting
		}
		// Now safe to tear down the old inbound.
		if err := c.removeOldTag(oldTag); err != nil {
			log.Printf("[%s: %d] warning: remove old tag %s after successful add: %s",
				newNodeInfo.NodeType, newNodeInfo.NodeID, oldTag, err)
		}
		if err := c.DeleteInboundLimiter(oldTag); err != nil {
			log.Print(err)
		}
		c.DeleteRule(oldTag)
		return nil
	}

	// Same tag: replace in place (cannot have two handlers with one tag).
	//
	// Helper to restore the previous inbound with its original users on failure.
	restoreOld := func(reason string, cause error) error {
		c.setState(cur.nodeInfo, oldTag)
		if restoreErr := c.addNewTag(cur.nodeInfo); restoreErr != nil {
			return fmt.Errorf("%s (%v) and restore old inbound failed (%v)", reason, cause, restoreErr)
		}
		if len(oldUsers) > 0 {
			if uerr := c.addNewUser(&oldUsers, cur.nodeInfo); uerr != nil {
				log.Printf("[%s: %d] restore users: %s",
					cur.nodeInfo.NodeType, cur.nodeInfo.NodeID, uerr)
			}
			if lerr := c.AddInboundLimiter(oldTag, cur.nodeInfo.SpeedLimit, &oldUsers); lerr != nil {
				log.Print(lerr)
			}
		}
		return fmt.Errorf("%s (restored old): %w", reason, cause)
	}

	if err := c.removeOldTag(oldTag); err != nil {
		return fmt.Errorf("remove inbound %s failed: %w", oldTag, err)
	}
	c.setState(newNodeInfo, newTag)
	if err := c.addNewTag(newNodeInfo); err != nil {
		return restoreOld("add new inbound failed", err)
	}
	if err := c.addNewUser(newUserInfo, newNodeInfo); err != nil {
		// Remove the half-built new inbound before restoring the old one.
		_ = c.removeOldTag(newTag)
		return restoreOld("add users to new inbound failed", err)
	}
	// Same tag: AddInboundLimiter Store()-replaces the whole InboundInfo for
	// this tag. Do NOT DeleteInboundLimiter(oldTag) here — oldTag == newTag,
	// so that would wipe the limiter we just installed.
	if err := c.AddInboundLimiter(newTag, newNodeInfo.SpeedLimit, newUserInfo); err != nil {
		log.Print(err) // non-fatal: node works without rate limiting
	}
	return nil
}

func (c *Controller) userInfoMonitor() error {
	// If Close() has been called, bail out early.
	if c.closeCtx.Err() != nil {
		return nil
	}

	var err error

	// Report server status
	if !c.config.DisableReportNodeStatus {
		var CPU, Mem, Disk float64
		var Uptime int
		CPU, Mem, Disk, Uptime, err = serverstatus.GetSystemInfo()
		if err != nil {
			log.Print(err)
		}
		// Read active connection count from the dispatcher's OnlineMap.
		dispatcher := c.server.GetFeature(routing.DispatcherType()).(*mydispatcher.DefaultDispatcher)
		activeConns := dispatcher.GetOnlineIPCount(c.getState().tag)

		if err = c.apiClient.ReportNodeStatus(&api.NodeStatus{
			CPU:         CPU,
			Mem:         Mem,
			Disk:        Disk,
			Uptime:      Uptime,
			ActiveConns: activeConns,
		}); err != nil {
			log.Print(err)
		}
	}

	// Get User traffic – snapshot list + tag once so a concurrent node
	// replace cannot mix counter keys mid-report.
	snapshot := c.getUserList()
	tag := c.getState().tag
	userEmailKey := func(email string, uid int) string {
		return fmt.Sprintf("%s|%s|%d", tag, email, uid)
	}

	userTraffic := make([]api.UserTraffic, 0, len(snapshot))
	// drainedKeys retains the exact counter email keys used when draining so
	// restore after a failed push targets the same counters.
	drainedKeys := make([]string, 0, len(snapshot))
	for i := range snapshot {
		user := &snapshot[i]
		key := userEmailKey(user.Email, user.UID)
		up, down := c.getTraffic(key)
		if up > 0 || down > 0 {
			userTraffic = append(userTraffic, api.UserTraffic{
				UID:      user.UID,
				Email:    user.Email,
				Upload:   up,
				Download: down,
			})
			drainedKeys = append(drainedKeys, key)
		}
	}
	if len(userTraffic) > 0 && !c.config.DisableUploadTraffic {
		if err = c.apiClient.ReportUserTraffic(&userTraffic); err != nil {
			log.Print(err)
			// Counters were already drained atomically — put the bytes back so
			// a transient panel/API failure does not permanently lose traffic.
			for i := range userTraffic {
				c.restoreTraffic(drainedKeys[i], userTraffic[i].Upload, userTraffic[i].Download)
			}
		}
	}

	// Report Online info.
	// Always send the report (even when empty) so the panel can
	// distinguish "0 users currently online" from "node is down".
	if onlineDevice, err := c.GetOnlineDevice(tag); err != nil {
		log.Print(err)
	} else {
		if err = c.apiClient.ReportNodeOnlineUsers(onlineDevice); err != nil {
			log.Print(err)
		} else {
			s := c.getState()
			log.Printf("[%s: %d] Report %d online users",
				s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(*onlineDevice))
		}
	}

	// Report Illegal user
	if detectResult, err := c.GetDetectResult(tag); err != nil {
		log.Print(err)
	} else if len(*detectResult) > 0 {
		if err = c.apiClient.ReportIllegal(detectResult); err != nil {
			log.Print(err)
		} else {
			s := c.getState()
			log.Printf("[%s: %d] Report %d illegal behaviors",
				s.nodeInfo.NodeType, s.nodeInfo.NodeID, len(*detectResult))
		}
	}
	return nil
}

// buildNodeTag is a pure function – no receiver needed.
func buildNodeTag(info *api.NodeInfo, listenIP string) string {
	return fmt.Sprintf("%s_%s_%d", info.NodeType, listenIP, info.Port)
}

// nodeInfoEqual reports whether two NodeInfo values are semantically identical.
// It avoids reflect.DeepEqual by comparing each field directly, which is
// measurably faster and avoids allocations on the hot monitoring path.
// json.RawMessage ([]byte) is not directly comparable with == so we use
// bytes.Equal for those fields.
func nodeInfoEqual(a, b *api.NodeInfo) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.NodeID == b.NodeID &&
		a.Port == b.Port &&
		a.NodeType == b.NodeType &&
		a.SpeedLimit == b.SpeedLimit &&
		a.AlterID == b.AlterID &&
		a.TransportProtocol == b.TransportProtocol &&
		a.FakeType == b.FakeType &&
		a.Host == b.Host &&
		a.Path == b.Path &&
		a.EnableTLS == b.EnableTLS &&
		a.TLSType == b.TLSType &&
		a.EnableVision == b.EnableVision &&
		a.EnableVless == b.EnableVless &&
		a.CypherMethod == b.CypherMethod &&
		a.ServiceName == b.ServiceName &&
		bytes.Equal(a.Header, b.Header) &&
		bytes.Equal(a.RealitySettings, b.RealitySettings)
}
