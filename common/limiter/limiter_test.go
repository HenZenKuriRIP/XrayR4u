package limiter

import (
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
)

func TestDeviceLimitRefcount(t *testing.T) {
	l := New()
	users := []api.UserInfo{{
		UID:         1,
		Email:       "u@test",
		DeviceLimit: 1,
	}}
	tag := "vless_0.0.0.0_443"
	if err := l.AddInboundLimiter(tag, 0, &users); err != nil {
		t.Fatal(err)
	}
	email := emailKey(tag, "u@test", 1)

	// First connection from IP-A — allowed.
	if _, _, reject := l.GetUserBucket(tag, email, "1.1.1.1"); reject {
		t.Fatal("first connection should be allowed")
	}
	// Second stream/connection from same IP — allowed (refcount++).
	if _, _, reject := l.GetUserBucket(tag, email, "1.1.1.1"); reject {
		t.Fatal("same IP second stream should be allowed")
	}
	// Different IP while first still has refs — rejected.
	if _, _, reject := l.GetUserBucket(tag, email, "2.2.2.2"); !reject {
		t.Fatal("second device should be rejected when limit=1")
	}

	// Close one stream of IP-A — refcount still 1, slot held.
	l.RemoveOnlineIP(tag, email, "1.1.1.1")
	if _, _, reject := l.GetUserBucket(tag, email, "2.2.2.2"); !reject {
		t.Fatal("second device should still be rejected while one stream remains")
	}

	// Close last stream — slot free.
	l.RemoveOnlineIP(tag, email, "1.1.1.1")
	if _, _, reject := l.GetUserBucket(tag, email, "2.2.2.2"); reject {
		t.Fatal("second device should be allowed after all refs released")
	}
}

func TestGetOnlineDeviceDoesNotClearLiveState(t *testing.T) {
	l := New()
	users := []api.UserInfo{{
		UID:         7,
		Email:       "live@test",
		DeviceLimit: 1,
	}}
	tag := "anytls_0.0.0.0_443"
	if err := l.AddInboundLimiter(tag, 0, &users); err != nil {
		t.Fatal(err)
	}
	email := emailKey(tag, "live@test", 7)

	if _, _, reject := l.GetUserBucket(tag, email, "10.0.0.1"); reject {
		t.Fatal("unexpected reject on first device")
	}

	online, err := l.GetOnlineDevice(tag)
	if err != nil {
		t.Fatal(err)
	}
	if len(*online) != 1 || (*online)[0].IP != "10.0.0.1" || (*online)[0].UID != 7 {
		t.Fatalf("unexpected online snapshot: %+v", *online)
	}

	// After report, live IP must still occupy the device slot.
	if _, _, reject := l.GetUserBucket(tag, email, "10.0.0.1"); reject {
		t.Fatal("existing live IP must remain allowed after GetOnlineDevice")
	}
	if _, _, reject := l.GetUserBucket(tag, email, "10.0.0.99"); !reject {
		t.Fatal("GetOnlineDevice must not clear live IPs (device limit would open)")
	}

	// Release both refs (initial + re-add above), then new IP is allowed.
	l.RemoveOnlineIP(tag, email, "10.0.0.1")
	l.RemoveOnlineIP(tag, email, "10.0.0.1")
	if _, _, reject := l.GetUserBucket(tag, email, "10.0.0.99"); reject {
		t.Fatal("new IP should be allowed after all live refs released")
	}
}
