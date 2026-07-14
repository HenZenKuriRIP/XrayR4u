package controller

import (
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
)

func TestCompareUserListUUIDChange(t *testing.T) {
	old := []api.UserInfo{
		{UID: 1, Email: "a@x", UUID: "uuid-old"},
		{UID: 2, Email: "b@x", UUID: "uuid-b"},
	}
	newList := []api.UserInfo{
		{UID: 1, Email: "a@x", UUID: "uuid-new"}, // UUID rotated
		{UID: 2, Email: "b@x", UUID: "uuid-b"},
		{UID: 3, Email: "c@x", UUID: "uuid-c"}, // added
	}

	deleted, added := compareUserList(old, newList)
	if len(deleted) != 1 || deleted[0].UID != 1 || deleted[0].UUID != "uuid-old" {
		t.Fatalf("deleted = %+v, want old UID 1 with uuid-old", deleted)
	}
	if len(added) != 2 {
		t.Fatalf("added = %+v, want 2 entries (uuid change + new user)", added)
	}
	// Ensure UUID-new and UID 3 are both in added.
	seen := map[int]string{}
	for _, u := range added {
		seen[u.UID] = u.UUID
	}
	if seen[1] != "uuid-new" || seen[3] != "uuid-c" {
		t.Fatalf("added map = %v", seen)
	}
}

func TestCompareUserListEmailChange(t *testing.T) {
	old := []api.UserInfo{{UID: 1, Email: "old@x", UUID: "u"}}
	newList := []api.UserInfo{{UID: 1, Email: "new@x", UUID: "u"}}
	deleted, added := compareUserList(old, newList)
	if len(deleted) != 1 || deleted[0].Email != "old@x" {
		t.Fatalf("deleted = %+v", deleted)
	}
	if len(added) != 1 || added[0].Email != "new@x" {
		t.Fatalf("added = %+v", added)
	}
}

func TestCompareUserListUnchanged(t *testing.T) {
	old := []api.UserInfo{{UID: 1, Email: "a@x", UUID: "u", SpeedLimit: 10}}
	newList := []api.UserInfo{{UID: 1, Email: "a@x", UUID: "u", SpeedLimit: 99}}
	deleted, added := compareUserList(old, newList)
	if len(deleted) != 0 || len(added) != 0 {
		t.Fatalf("speed-only change should not delete/add: deleted=%+v added=%+v", deleted, added)
	}
}
