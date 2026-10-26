package rule

import (
	"regexp"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
)

func TestDetect_CountsAndDrain(t *testing.T) {
	rm := New()
	rules := []api.DetectRule{
		{ID: 7, Pattern: regexp.MustCompile(`(?i)bad\.example`)},
	}
	if err := rm.UpdateRule("tag1", rules); err != nil {
		t.Fatal(err)
	}
	email := "tag1|user@x|42"
	if !rm.Detect("tag1", "tcp:bad.example:443", email) {
		t.Fatal("expected reject")
	}
	if !rm.Detect("tag1", "tcp:bad.example:80", email) {
		t.Fatal("expected reject 2")
	}
	if rm.Detect("tag1", "tcp:good.example:443", email) {
		t.Fatal("expected allow")
	}
	res, err := rm.GetDetectResult("tag1")
	if err != nil {
		t.Fatal(err)
	}
	if len(*res) != 1 {
		t.Fatalf("entries=%d want 1", len(*res))
	}
	if (*res)[0].UID != 42 || (*res)[0].RuleID != 7 || (*res)[0].Count != 2 {
		t.Fatalf("%+v", (*res)[0])
	}
	// drained
	res2, _ := rm.GetDetectResult("tag1")
	if len(*res2) != 0 {
		t.Fatalf("expected empty after drain")
	}
}
