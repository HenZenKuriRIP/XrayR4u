// Package limiter is to control the links that go into the dispatcher
package limiter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/juju/ratelimit"
	"github.com/xtls/xray-core/common/errors"
)

// UserInfo is the per-user limiter configuration stored inside InboundInfo.
type UserInfo struct {
	UID         int
	SpeedLimit  uint64
	DeviceLimit int32
}

// ipEntry tracks one online source IP with a connection/stream reference count.
// refs is incremented on each GetUserBucket (new stream/connection) and
// decremented on RemoveOnlineIP (stream/connection close). The IP is only
// removed from the map when refs reaches zero, so AnyTLS mux and multi-conn
// from the same IP cannot free the device slot early.
type ipEntry struct {
	uid  int
	refs int
}

// userOnlineState is the per-user online-device tracker.
// All mutations are protected by mu so check+add is always atomic.
type userOnlineState struct {
	mu  sync.Mutex
	ips map[string]*ipEntry // ip → entry
}

// tryAdd registers ip for uid under mu, incrementing the refcount when the IP
// is already present. Returns false (without recording the IP) if the device
// limit would be exceeded by a *new* IP.
func (s *userOnlineState) tryAdd(ip string, uid int, limit int32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, exists := s.ips[ip]; exists {
		e.refs++
		if e.uid == 0 {
			e.uid = uid
		}
		return true
	}
	if limit > 0 && int32(len(s.ips)) >= limit {
		return false
	}
	s.ips[ip] = &ipEntry{uid: uid, refs: 1}
	return true
}

// remove decrements the refcount for ip. The IP is deleted only when no
// connections/streams still hold it. Safe to call for unknown IPs (no-op).
func (s *userOnlineState) remove(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, exists := s.ips[ip]
	if !exists {
		return
	}
	e.refs--
	if e.refs <= 0 {
		delete(s.ips, ip)
	}
}

// copyAll returns a private snapshot of (ip → uid) without clearing state.
// Device-limit slots stay occupied for live connections; online reporting
// reflects currently tracked concurrent devices.
func (s *userOnlineState) copyAll() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.ips))
	for ip, e := range s.ips {
		out[ip] = e.uid
	}
	return out
}

// InboundInfo holds the per-inbound state for the limiter.
type InboundInfo struct {
	tag            string
	nodeSpeedLimit uint64

	userInfo  sync.Map // key: emailKey string → UserInfo
	bucketHub sync.Map // key: emailKey string → *ratelimit.Bucket

	// onlineIPs maps email string → *userOnlineState (created lazily).
	onlineIPs sync.Map
}

// Limiter is the global limiter registry, one per DefaultDispatcher.
type Limiter struct {
	inboundInfo sync.Map // key: tag → *InboundInfo
}

func New() *Limiter {
	return &Limiter{}
}

// emailKey returns the composite key used for all per-user maps.
func emailKey(tag, email string, uid int) string {
	return fmt.Sprintf("%s|%s|%d", tag, email, uid)
}

func (l *Limiter) AddInboundLimiter(tag string, nodeSpeedLimit uint64, userList *[]api.UserInfo) error {
	info := &InboundInfo{
		tag:            tag,
		nodeSpeedLimit: nodeSpeedLimit,
	}
	for _, u := range *userList {
		info.userInfo.Store(emailKey(tag, u.Email, u.UID), UserInfo{
			UID:         u.UID,
			SpeedLimit:  u.SpeedLimit,
			DeviceLimit: int32(u.DeviceLimit),
		})
	}
	l.inboundInfo.Store(tag, info)
	return nil
}

func (l *Limiter) UpdateInboundLimiter(tag string, updatedUserList *[]api.UserInfo) error {
	v, ok := l.inboundInfo.Load(tag)
	if !ok {
		return fmt.Errorf("no such inbound in limiter: %s", tag)
	}
	info := v.(*InboundInfo)
	for _, u := range *updatedUserList {
		k := emailKey(tag, u.Email, u.UID)
		info.userInfo.Store(k, UserInfo{
			UID:         u.UID,
			SpeedLimit:  u.SpeedLimit,
			DeviceLimit: int32(u.DeviceLimit),
		})
		info.bucketHub.Delete(k) // invalidate stale rate bucket
	}
	return nil
}

// DeleteUsersFromLimiter removes deleted users' state from the limiter.
// It must be called after removeUsers() so that a departing user's
// deviceLimit / speedLimit entries don't persist in the limiter maps.
func (l *Limiter) DeleteUsersFromLimiter(tag string, deletedUsers []api.UserInfo) error {
	v, ok := l.inboundInfo.Load(tag)
	if !ok {
		// Inbound may have been removed already; not an error.
		return nil
	}
	info := v.(*InboundInfo)
	for _, u := range deletedUsers {
		k := emailKey(tag, u.Email, u.UID)
		info.userInfo.Delete(k)
		info.bucketHub.Delete(k)
		info.onlineIPs.Delete(k)
	}
	return nil
}

func (l *Limiter) DeleteInboundLimiter(tag string) error {
	l.inboundInfo.Delete(tag)
	return nil
}

// GetOnlineDevice collects currently online users for a tag without clearing
// the live device-limit state. Empty per-user trackers are pruned to avoid
// unbounded map growth.
func (l *Limiter) GetOnlineDevice(tag string) (*[]api.OnlineUser, error) {
	v, ok := l.inboundInfo.Load(tag)
	if !ok {
		return nil, fmt.Errorf("no such inbound in limiter: %s", tag)
	}
	info := v.(*InboundInfo)

	var onlineUsers []api.OnlineUser

	info.onlineIPs.Range(func(key, value any) bool {
		state := value.(*userOnlineState)
		m := state.copyAll()
		for ip, uid := range m {
			onlineUsers = append(onlineUsers, api.OnlineUser{UID: uid, IP: ip})
		}
		if len(m) == 0 {
			// No live connections — clean up idle bucket and stale entry.
			info.bucketHub.Delete(key.(string))
			info.onlineIPs.Delete(key.(string))
		}
		return true
	})

	return &onlineUsers, nil
}

// RemoveOnlineIP releases one connection/stream reference for the given IP.
// The device-limit slot is freed only when the last reference is released.
//
// It is safe to call for an IP that was never added (no-op) and for a tag/email
// that does not exist (no-op).
func (l *Limiter) RemoveOnlineIP(tag string, email string, ip string) {
	v, ok := l.inboundInfo.Load(tag)
	if !ok {
		return
	}
	info := v.(*InboundInfo)
	if sv, ok := info.onlineIPs.Load(email); ok {
		sv.(*userOnlineState).remove(ip)
	}
}

// GetUserBucket is called on the hot path (every new connection).
// It enforces device limits and returns the rate-limit bucket for the user.
func (l *Limiter) GetUserBucket(tag string, email string, ip string) (bucket *ratelimit.Bucket, speedLimit bool, reject bool) {
	v, ok := l.inboundInfo.Load(tag)
	if !ok {
		errors.LogDebug(context.Background(), "GetUserBucket: inbound not found: ", tag)
		return nil, false, false
	}
	info := v.(*InboundInfo)

	// Resolve per-user limits (zero means "no limit").
	var userLimit uint64
	var deviceLimit int32
	var uid int
	if uv, ok := info.userInfo.Load(email); ok {
		u := uv.(UserInfo)
		uid = u.UID
		userLimit = u.SpeedLimit
		deviceLimit = u.DeviceLimit
	}

	// --- Device-limit enforcement (race-free, refcounted) ---
	// Load or create the per-user online-state object.
	// IMPORTANT: whether we win or lose the LoadOrStore race, we must
	// call tryAdd (which holds state.mu) to register the IP.
	newState := &userOnlineState{ips: make(map[string]*ipEntry)}
	actual, _ := info.onlineIPs.LoadOrStore(email, newState)
	state := actual.(*userOnlineState)
	// tryAdd is always safe: it holds state.mu for the check+insert.
	if !state.tryAdd(ip, uid, deviceLimit) {
		return nil, false, true // device limit exceeded
	}

	// --- Speed-limit bucket (zero-allocation fast path) ---
	limit := determineRate(info.nodeSpeedLimit, userLimit)
	if limit == 0 {
		return nil, false, false
	}

	// Fast path: bucket already exists.
	if bv, ok := info.bucketHub.Load(email); ok {
		return bv.(*ratelimit.Bucket), true, false
	}

	// Slow path: create and race to store.
	log.Printf("[limiter] creating rate bucket email=%s limit=%d Bps (%.0f Mbps)", email, limit, float64(limit)*8/1000000)
	newBucket := ratelimit.NewBucketWithQuantum(100*time.Millisecond, int64(limit)/10, int64(limit)/10)
	if bv, loaded := info.bucketHub.LoadOrStore(email, newBucket); loaded {
		// Another goroutine won the race; discard ours.
		return bv.(*ratelimit.Bucket), true, false
	}
	return newBucket, true, false
}

// determineRate returns the effective byte-per-second rate limit.
// When only one side is set (non-zero), that side wins.
// When both are set, the stricter (smaller) limit wins.
func determineRate(nodeLimit, userLimit uint64) uint64 {
	switch {
	case nodeLimit == 0 && userLimit == 0:
		return 0
	case nodeLimit == 0:
		return userLimit
	case userLimit == 0:
		return nodeLimit
	default:
		if nodeLimit < userLimit {
			return nodeLimit
		}
		return userLimit
	}
}
