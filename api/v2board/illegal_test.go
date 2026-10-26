package v2board

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/bitly/go-simplejson"
	"github.com/stretchr/testify/require"
)

func TestAggregateIllegalBody_CountsAndCap(t *testing.T) {
	t.Parallel()
	in := []api.DetectResult{
		{UID: 1, RuleID: 3, Count: 2},
		{UID: 1, RuleID: 3, Count: 5}, // merge → 7
		{UID: 2, RuleID: 1},           // count default 1
	}
	body := aggregateIllegalBody(in)
	require.Len(t, body, 2)
	by := map[int]int{}
	for _, row := range body {
		by[row["uid"]*1000+row["rule_id"]] = row["count"]
	}
	require.Equal(t, 7, by[1*1000+3])
	require.Equal(t, 1, by[2*1000+1])

	// Cap: more than maxIllegalReportItems unique keys → truncated by count.
	big := make([]api.DetectResult, 0, maxIllegalReportItems+50)
	for i := 0; i < maxIllegalReportItems+50; i++ {
		big = append(big, api.DetectResult{UID: i + 1, RuleID: 1, Count: i + 1})
	}
	capped := aggregateIllegalBody(big)
	require.Len(t, capped, maxIllegalReportItems)
	require.GreaterOrEqual(t, capped[0]["count"], capped[len(capped)-1]["count"])
}

func TestParsePanelRuleList_ObjectAndEnable(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"version": 1730000000,
		"rules": [
			{"id": 1, "pattern": "(?i)tracker\\.", "enable": true},
			{"id": 2, "pattern": "disabled", "enable": false},
			{"id": 3, "pattern": ""},
			{"id": 4, "pattern": "ok-rule"}
		]
	}`)
	j, err := simplejson.NewJson(raw)
	require.NoError(t, err)
	rules := parsePanelRuleList(j)
	require.Len(t, rules, 2) // id 1 and 4; 2 disabled, 3 empty
	ids := map[int]bool{}
	for _, r := range rules {
		ids[r.ID] = true
		require.NotNil(t, r.Pattern)
	}
	require.True(t, ids[1])
	require.True(t, ids[4])
}

func TestParseUIDList(t *testing.T) {
	t.Parallel()
	j, err := simplejson.NewJson([]byte(`[12, "88", 12, 0, -1, 99]`))
	require.NoError(t, err)
	uids := parseUIDList(j)
	require.Equal(t, []int{12, 88, 99}, uids)

	// float-form numbers from PHP/json_encode
	j2, err := simplejson.NewJson([]byte(`[7.0, 8]`))
	require.NoError(t, err)
	require.Equal(t, []int{7, 8}, parseUIDList(j2))
}

func TestReportIllegal_HTTPAndKickUIDs(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	var gotAuth, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/server/UniProxy/illegal", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("node_id"))
		require.Empty(t, r.URL.Query().Get("token"), "token must not be in query")
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("X-Api-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok","accepted":2,"dropped":0,"schema":1,
			"kick_uids":[12],"disable_uids":[12,88]
		}`))
	}))
	t.Cleanup(srv.Close)

	c := New(&api.Config{
		APIHost:  srv.URL,
		Key:      "node-secret",
		NodeID:   1,
		NodeType: "anytls",
		Timeout:  3,
	})
	list := []api.DetectResult{
		{UID: 12, RuleID: 3, Count: 2},
		{UID: 88, RuleID: 1, Count: 1},
	}
	require.NoError(t, c.ReportIllegal(&list))
	require.True(t, strings.HasPrefix(gotAuth, "Bearer "))
	require.Equal(t, "node-secret", gotKey)

	var posted []map[string]int
	require.NoError(t, json.Unmarshal(gotBody, &posted))
	require.Len(t, posted, 2)

	kick := c.TakeKickUIDs()
	require.ElementsMatch(t, []int{12, 88}, kick)
	require.Empty(t, c.TakeKickUIDs())
}

func TestReportIllegal_FailureNoFalseSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(&api.Config{
		APIHost: srv.URL, Key: "k", NodeID: 1, NodeType: "vless", Timeout: 2,
	})
	list := []api.DetectResult{{UID: 1, RuleID: 1, Count: 1}}
	err := c.ReportIllegal(&list)
	require.Error(t, err)
	require.Empty(t, c.TakeKickUIDs())
}

func TestFetchPanelRules_FailSoftUsesCache(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/server/UniProxy/rule", r.URL.Path)
		hits++
		if hits >= 2 {
			http.Error(w, "down", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":100,"rules":[{"id":7,"pattern":"bad\\.host"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(&api.Config{
		APIHost: srv.URL, Key: "k", NodeID: 2, NodeType: "anytls", Timeout: 2,
	})

	r1, err := c.fetchPanelRules()
	require.NoError(t, err)
	require.Len(t, r1, 1)
	require.Equal(t, 7, r1[0].ID)

	// Fail soft: use cache
	r2, err := c.fetchPanelRules()
	require.NoError(t, err)
	require.Len(t, r2, 1)
	require.Equal(t, 7, r2[0].ID)
	require.GreaterOrEqual(t, hits, 2)
}

func TestFetchPanelRules_VersionSkipRecompile(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":42,"rules":[{"id":1,"pattern":"x"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(&api.Config{
		APIHost: srv.URL, Key: "k", NodeID: 1, NodeType: "anytls", Timeout: 2,
	})
	r1, err := c.fetchPanelRules()
	require.NoError(t, err)
	require.Len(t, r1, 1)
	pat1 := r1[0].Pattern

	r2, err := c.fetchPanelRules()
	require.NoError(t, err)
	require.Len(t, r2, 1)
	// Same compiled pointer from cache (version unchanged)
	require.Equal(t, pat1, r2[0].Pattern)
	require.Equal(t, 2, hits)
}

func TestGetNodeRule_MergesLocalAndPanel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":1,"rules":[{"id":9,"pattern":"panel-rule"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(&api.Config{
		APIHost: srv.URL, Key: "k", NodeID: 1, NodeType: "anytls", Timeout: 2,
	})
	c.LocalRuleList = []api.DetectRule{
		{ID: -1, Pattern: regexp.MustCompile("local-only")},
	}

	all, err := c.GetNodeRule()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(*all), 2)
	ids := map[int]bool{}
	for _, r := range *all {
		ids[r.ID] = true
	}
	require.True(t, ids[-1])
	require.True(t, ids[9])
}

func TestReportIllegal_EmptyListNoOp(t *testing.T) {
	t.Parallel()
	c := New(&api.Config{
		APIHost: "http://127.0.0.1:1", Key: "k", NodeID: 1, NodeType: "anytls", Timeout: 1,
	})
	empty := []api.DetectResult{}
	require.NoError(t, c.ReportIllegal(&empty))
	require.NoError(t, c.ReportIllegal(nil))
}
