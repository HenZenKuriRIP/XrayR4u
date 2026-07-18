package tools

import "testing"

func TestIsToolsInvocation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"XrayR"}, false},
		{[]string{"XrayR", "--config", "c.yml"}, false},
		{[]string{"XrayR", "tools"}, true},
		{[]string{"XrayR", "tool", "x25519"}, true},
		{[]string{"XrayR", "x25519"}, true},
		{[]string{"XrayR", "mldsa65"}, true},
		{[]string{"XrayR", "vlessenc"}, true},
		{[]string{"XrayR", "uuid"}, true},
		{[]string{"XrayR", "help"}, true},
		{[]string{"XrayR", "run"}, false},
	}
	for _, tc := range cases {
		if got := IsToolsInvocation(tc.args); got != tc.want {
			t.Fatalf("IsToolsInvocation(%v)=%v want %v", tc.args, got, tc.want)
		}
	}
}
