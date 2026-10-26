package v2board

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/common/regexutil"
	"github.com/bitly/go-simplejson"
	"github.com/go-resty/resty/v2"
)

// maxIllegalReportItems is the panel contract upper bound for one POST /illegal body.
const maxIllegalReportItems = 1000

// APIClient create an api client to the panel.
type APIClient struct {
	client        *resty.Client
	APIHost       string
	NodeID        int
	Key           string
	NodeType      string
	EnableReality bool
	SpeedLimit    float64
	DeviceLimit   int
	LocalRuleList []api.DetectRule
	ConfigResp    *simplejson.Json
	access        sync.Mutex
	// lastKickUIDs from panel illegal policy response (cleared by TakeKickUIDs).
	// Merges kick_uids + disable_uids (both mean "drop live sessions now").
	lastKickUIDs []int

	// Panel detect-rule cache (GET /UniProxy/rule). Avoids dropping rules on
	// transient panel errors and skips recompile when version is unchanged.
	panelRuleVersion int64
	panelRulesCache  []api.DetectRule
	panelRulesOK     bool
}

// New create an api instance
func New(apiConfig *api.Config) *APIClient {

	client := resty.New()
	client.SetRetryCount(3)
	if apiConfig.Timeout > 0 {
		client.SetTimeout(time.Duration(apiConfig.Timeout) * time.Second)
	} else {
		client.SetTimeout(5 * time.Second)
	}
	client.SetTLSClientConfig(&tls.Config{
		MinVersion: tls.VersionTLS12,
	})
	client.OnError(func(req *resty.Request, err error) {
		if v, ok := err.(*resty.ResponseError); ok {
			// Never log raw resty errors that may embed full URLs with secrets.
			log.Print(redactError(v.Err))
		}
	})
	client.SetBaseURL(apiConfig.APIHost)
	// node_id stays in the query string (non-secret). API credentials go in
	// headers only — panel accepts Authorization: Bearer and X-Api-Key.
	// Putting the token in the URL leaked it via reverse-proxy access logs,
	// Resty retries, and APM traces.
	client.SetQueryParams(map[string]string{
		"node_id": strconv.Itoa(apiConfig.NodeID),
	})
	if apiConfig.Key != "" {
		client.SetHeader("Authorization", "Bearer "+apiConfig.Key)
		client.SetHeader("X-Api-Key", apiConfig.Key)
	}
	// Read local rule list
	localRuleList := readLocalRuleList(apiConfig.RuleListPath)

	apiClient := &APIClient{
		client:        client,
		NodeID:        apiConfig.NodeID,
		Key:           apiConfig.Key,
		APIHost:       apiConfig.APIHost,
		NodeType:      apiConfig.NodeType,
		EnableReality: apiConfig.EnableReality,
		SpeedLimit:    apiConfig.SpeedLimit,
		DeviceLimit:   apiConfig.DeviceLimit,
		LocalRuleList: localRuleList,
	}
	return apiClient
}

// readLocalRuleList reads the local rule list file
func readLocalRuleList(path string) (LocalRuleList []api.DetectRule) {
	LocalRuleList = make([]api.DetectRule, 0)
	if path == "" {
		return LocalRuleList
	}

	file, err := os.Open(path)
	if err != nil {
		log.Printf("Error when opening rule list file: %s", err)
		return LocalRuleList
	}
	defer file.Close()

	fileScanner := bufio.NewScanner(file)
	for fileScanner.Scan() {
		LocalRuleList = append(LocalRuleList, api.DetectRule{
			ID:      -1,
			Pattern: regexutil.SafeCompileOrDefault(fileScanner.Text()),
		})
	}
	// Optional local rules must not kill the process on read errors.
	if err := fileScanner.Err(); err != nil {
		log.Printf("Error while reading rule list file: %s", err)
		return make([]api.DetectRule, 0)
	}
	return LocalRuleList
}

// Describe return a description of the client.
// Key is redacted so accidental logging/serialization cannot leak the API token.
func (c *APIClient) Describe() api.ClientInfo {
	return api.ClientInfo{
		APIHost:  c.APIHost,
		NodeID:   c.NodeID,
		Key:      redactKey(c.Key),
		NodeType: c.NodeType,
	}
}

func redactKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****"
}

// sensitiveJSONKeys are stripped (or redacted) before panel JSON is written
// into error strings / logs.
var sensitiveJSONKeys = map[string]struct{}{
	"private_key":   {},
	"privateKey":    {},
	"mldsa65_seed":  {},
	"mldsa65Seed":   {},
	"token":         {},
	"password":      {},
	"secret":        {},
	"api_key":       {},
	"apiKey":        {},
	"Authorization": {},
}

// redactSensitiveValue walks a decoded JSON tree and replaces known secret
// fields with "****". Non-object leaves are left intact.
func redactSensitiveValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, child := range t {
			if _, hit := sensitiveJSONKeys[k]; hit {
				out[k] = "****"
				continue
			}
			out[k] = redactSensitiveValue(child)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, child := range t {
			out[i] = redactSensitiveValue(child)
		}
		return out
	default:
		return v
	}
}

// safeJSONForLog returns a redacted, length-capped JSON snapshot suitable for
// error messages. Never include raw panel bodies that may carry REALITY keys.
func safeJSONForLog(j *simplejson.Json) string {
	if j == nil {
		return "<nil>"
	}
	raw, err := j.MarshalJSON()
	if err != nil {
		return "<unmarshallable>"
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return truncateForLog(string(raw), 256)
	}
	redacted, err := json.Marshal(redactSensitiveValue(decoded))
	if err != nil {
		return "<redact-failed>"
	}
	return truncateForLog(string(redacted), 512)
}

func truncateForLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// redactURL masks secret query parameters that legacy code or proxies may still
// embed in error strings.
func redactURL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for _, key := range []string{"token", "key", "password", "secret", "api_key"} {
		if q.Has(key) {
			q.Set(key, "****")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func redactError(err error) string {
	if err == nil {
		return ""
	}
	return redactURL(err.Error())
}

// Debug enables lightweight request tracing without Resty's full HTTP dump,
// which would print Authorization / X-Api-Key headers in cleartext.
func (c *APIClient) Debug() {
	// Intentionally not calling SetDebug(true): Resty dumps headers + bodies.
	c.client.OnBeforeRequest(func(_ *resty.Client, req *resty.Request) error {
		log.Printf("v2board debug: %s %s", req.Method, redactURL(req.URL))
		return nil
	})
}

func (c *APIClient) assembleURL(path string) string {
	return c.APIHost + path
}

// parseResponse validates and decodes a resty response.
// Passing the reqErr from the resty call as err allows the function to be the
// single point of error handling: if the request itself failed, or the
// response is nil or carries a ≥ 400 status, an error is returned.
func (c *APIClient) parseResponse(res *resty.Response, path string, err error) (*simplejson.Json, error) {
	url := redactURL(c.assembleURL(path))
	if err != nil {
		return nil, fmt.Errorf("request %s failed: %s", url, redactError(err))
	}
	if res == nil {
		return nil, fmt.Errorf("request %s returned nil response", url)
	}
	if res.StatusCode() >= 400 {
		// Cap body length so error logs cannot dump large/sensitive panel responses.
		msg := truncateForLog(string(res.Body()), 256)
		return nil, fmt.Errorf("request %s failed: status=%d body=%s", url, res.StatusCode(), msg)
	}
	rtn, err := simplejson.NewJson(res.Body())
	if err != nil {
		return nil, fmt.Errorf("request %s returned invalid JSON: %s", url, truncateForLog(string(res.Body()), 128))
	}
	return rtn, nil
}

// GetNodeInfo will pull NodeInfo Config from panel
func (c *APIClient) GetNodeInfo() (nodeInfo *api.NodeInfo, err error) {
	path := "/api/v1/server/UniProxy/config"

	req := c.client.R().
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)
	req.SetQueryParam("local_port", "1")

	// Split reqErr from the response-decode err so the resty error cannot
	// be silently replaced by parseResponse's own error (M-3 shadowing fix).
	res, reqErr := req.Get(path)
	response, err := c.parseResponse(res, path, reqErr)
	c.access.Lock()
	defer c.access.Unlock()
	// Only update ConfigResp on success; a failed fetch must not overwrite a
	// previously valid config with nil and break GetNodeRule.
	if response != nil {
		c.ConfigResp = response
	}
	if err != nil {
		return nil, err
	}

	nodeInfo, err = c.ParseUniProxyNodeResponse(response)
	if err != nil {
		// Do not dump raw panel JSON: tls_settings may contain private_key /
		// mldsa65_seed. Log a redacted field summary only.
		return nil, fmt.Errorf("Parse node info failed: %s, body=%s", err, safeJSONForLog(response))
	}

	return nodeInfo, nil
}

// GetUserList will pull user from panel
func (c *APIClient) GetUserList() (UserList *[]api.UserInfo, err error) {
	path := "/api/v1/server/UniProxy/user"

	req := c.client.R().
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)

	res, reqErr := req.Get(path)
	response, err := c.parseResponse(res, path, reqErr)
	if err != nil {
		return nil, err
	}

	return c.ParseUniProxyUserResponse(response)
}

// ReportUserTraffic reports the user traffic.
// K2Board expects format: {user_id: [upload, download]}.
func (c *APIClient) ReportUserTraffic(userTraffic *[]api.UserTraffic) error {
	path := "/api/v1/server/UniProxy/push"

	data := make(map[int][]int64)
	for _, traffic := range *userTraffic {
		data[traffic.UID] = []int64{traffic.Upload, traffic.Download}
	}

	req := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetBody(data).
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)

	res, reqErr := req.Post(path)
	_, err := c.parseResponse(res, path, reqErr)
	if err != nil {
		return err
	}
	return nil
}

// GetNodeRule implements the API interface.
// Merges: local RuleListPath (id=-1) + GET /UniProxy/rule + optional config routes.rules.
// Empty panel rules is success (local-only / no intercept). Never fails the
// controller path solely because the panel is briefly unreachable — last good
// panel rules are reused when available.
func (c *APIClient) GetNodeRule() (*[]api.DetectRule, error) {
	ruleList := make([]api.DetectRule, 0, len(c.LocalRuleList)+8)
	ruleList = append(ruleList, c.LocalRuleList...)

	// Panel rules (K2Board P1+): GET /api/v1/server/UniProxy/rule
	// Response: {"version":N,"rules":[{"id":1,"pattern":"..."},...]}
	panelRules, err := c.fetchPanelRules()
	if err != nil {
		// No cache yet (old panel without /rule, or first boot offline).
		// Local rules still apply; do not treat as hard error for the node.
		log.Printf("GetNodeRule panel fetch: %s", redactError(err))
	} else {
		ruleList = append(ruleList, panelRules...)
	}

	// Optional legacy: UniProxy config response "routes.rules[].domain"
	c.access.Lock()
	cfg := c.ConfigResp
	c.access.Unlock()
	if cfg != nil {
		routes := cfg.Get("routes")
		if rulesArray, ok := routes.CheckGet("rules"); ok {
			for i := range rulesArray.MustArray() {
				rule := rulesArray.GetIndex(i)
				if domain, ok := rule.CheckGet("domain"); ok {
					for _, d := range domain.MustStringArray() {
						if d == "" {
							continue
						}
						ruleList = append(ruleList, api.DetectRule{
							ID:      i,
							Pattern: regexutil.SafeCompileOrDefault(d),
						})
					}
				}
			}
		}
	}
	return &ruleList, nil
}

// fetchPanelRules loads K2Board detect rules with version cache + fail-soft.
// Empty "rules":[] is valid and clears the panel portion of the rule set.
func (c *APIClient) fetchPanelRules() ([]api.DetectRule, error) {
	path := "/api/v1/server/UniProxy/rule"
	req := c.client.R().ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)
	res, reqErr := req.Get(path)
	response, err := c.parseResponse(res, path, reqErr)
	if err != nil {
		// Transient failure: keep last successful panel rules so audit does not
		// silently open after a blip. First failure with no cache returns err.
		c.access.Lock()
		if c.panelRulesOK {
			cached := append([]api.DetectRule(nil), c.panelRulesCache...)
			ver := c.panelRuleVersion
			c.access.Unlock()
			log.Printf("GetNodeRule panel fetch failed, using cached rules (version=%d): %s",
				ver, redactError(err))
			return cached, nil
		}
		c.access.Unlock()
		return nil, err
	}

	version := jsonToInt64(response.Get("version"))

	// Optional optimization: same version → reuse compiled patterns.
	c.access.Lock()
	if c.panelRulesOK && version != 0 && version == c.panelRuleVersion {
		cached := append([]api.DetectRule(nil), c.panelRulesCache...)
		c.access.Unlock()
		return cached, nil
	}
	c.access.Unlock()

	out := parsePanelRuleList(response)

	c.access.Lock()
	c.panelRuleVersion = version
	c.panelRulesCache = append([]api.DetectRule(nil), out...)
	c.panelRulesOK = true
	c.access.Unlock()
	return out, nil
}

// parsePanelRuleList accepts {"rules":[...]} or a bare array. Invalid patterns
// are skipped (SafeCompileOrDefault → nil); Detect ignores nil Pattern.
func parsePanelRuleList(response *simplejson.Json) []api.DetectRule {
	out := make([]api.DetectRule, 0)
	if response == nil {
		return out
	}
	appendRule := func(item *simplejson.Json) {
		if item == nil {
			return
		}
		// Skip disabled rules when panel sends enable:false
		if en, ok := item.CheckGet("enable"); ok {
			switch v := en.Interface().(type) {
			case bool:
				if !v {
					return
				}
			case string:
				if strings.EqualFold(v, "false") || v == "0" {
					return
				}
			}
		}
		pat := strings.TrimSpace(item.Get("pattern").MustString())
		if pat == "" {
			return
		}
		// action is informational for v1; node always rejects on match.
		out = append(out, api.DetectRule{
			ID:      int(jsonToInt64(item.Get("id"))),
			Pattern: regexutil.SafeCompileOrDefault(pat),
		})
	}

	if rules, ok := response.CheckGet("rules"); ok {
		for i := range rules.MustArray() {
			appendRule(rules.GetIndex(i))
		}
		return out
	}
	if arr, err := response.Array(); err == nil {
		for i := range arr {
			appendRule(response.GetIndex(i))
		}
	}
	return out
}

// jsonToInt64 robustly reads JSON numbers (float64 / json.Number / int).
func jsonToInt64(j *simplejson.Json) int64 {
	if j == nil {
		return 0
	}
	return interfaceToInt64(j.Interface())
}

func interfaceToInt64(v interface{}) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return int64(f)
	case float64:
		return int64(t)
	case float32:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case int32:
		return int64(t)
	case uint64:
		return int64(t)
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

// parseUIDList reads a JSON array of user ids (kick_uids / disable_uids).
func parseUIDList(j *simplejson.Json) []int {
	if j == nil {
		return nil
	}
	arr, err := j.Array()
	if err != nil || len(arr) == 0 {
		return nil
	}
	out := make([]int, 0, len(arr))
	seen := make(map[int]struct{}, len(arr))
	for _, el := range arr {
		uid := int(interfaceToInt64(el))
		if uid <= 0 {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	return out
}

// aggregateIllegalBody builds UniProxy v1 body: [{uid,rule_id,count}, ...]
// capped at maxIllegalReportItems (highest counts kept).
func aggregateIllegalBody(list []api.DetectResult) []map[string]int {
	type key struct{ uid, rule int }
	agg := make(map[key]int, len(list))
	for _, r := range list {
		n := r.Count
		if n <= 0 {
			n = 1
		}
		agg[key{r.UID, r.RuleID}] += n
	}
	body := make([]map[string]int, 0, len(agg))
	for k, n := range agg {
		body = append(body, map[string]int{
			"uid":     k.uid,
			"rule_id": k.rule,
			"count":   n,
		})
	}
	if len(body) <= maxIllegalReportItems {
		return body
	}
	// Keep highest-count items so risk signal is preserved under cap.
	sort.Slice(body, func(i, j int) bool {
		if body[i]["count"] == body[j]["count"] {
			if body[i]["uid"] == body[j]["uid"] {
				return body[i]["rule_id"] < body[j]["rule_id"]
			}
			return body[i]["uid"] < body[j]["uid"]
		}
		return body[i]["count"] > body[j]["count"]
	})
	return body[:maxIllegalReportItems]
}

// ReportNodeStatus implements the API interface.
// Reports system status (CPU/memory/disk/uptime) to the panel.
// This is a best-effort report: errors are returned to the caller (which logs
// them) but do not affect other operations.
func (c *APIClient) ReportNodeStatus(nodeStatus *api.NodeStatus) (err error) {
	path := "/api/v1/server/UniProxy/status"

	payload := NodeStatus{
		CPU:         nodeStatus.CPU,
		Mem:         nodeStatus.Mem,
		Disk:        nodeStatus.Disk,
		Uptime:      nodeStatus.Uptime,
		ActiveConns: nodeStatus.ActiveConns,
	}

	req := c.client.R().
		SetBody(payload).
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)

	res, reqErr := req.Post(path)
	_, err = c.parseResponse(res, path, reqErr)
	if err != nil {
		return err
	}
	return nil
}

// ReportNodeOnlineUsers implements the API interface.
// POSTs to /api/v1/server/UniProxy/alive with format {uid: [ip1, ip2, ...]}.
func (c *APIClient) ReportNodeOnlineUsers(onlineUserList *[]api.OnlineUser) error {
	path := "/api/v1/server/UniProxy/alive"
	data := make(map[int][]string)
	for _, user := range *onlineUserList {
		data[user.UID] = append(data[user.UID], user.IP)
	}

	req := c.client.R().
		SetBody(data).
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)

	res, err := req.Post(path)
	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}
	return nil
}

// ReportIllegal POSTs detect hits to the panel (K2Board UniProxy P0+).
//
//	POST /api/v1/server/UniProxy/illegal
//	Body v1: [{"uid":1,"rule_id":2,"count":3}, ...]
//
// Local intercept already happened in mydispatcher → RuleManager.Detect.
// This call only accounts to the panel. Failures must surface as errors so the
// controller logs them and does NOT print a false "Report N illegal… to panel".
// On HTTP success, optional P3 fields kick_uids / disable_uids are stored for
// the controller (TakeKickUIDs) to drop live sessions without touching
// subscription tokens.
func (c *APIClient) ReportIllegal(detectResultList *[]api.DetectResult) error {
	if detectResultList == nil || len(*detectResultList) == 0 {
		return nil
	}
	path := "/api/v1/server/UniProxy/illegal"

	body := aggregateIllegalBody(*detectResultList)
	if len(body) == 0 {
		return nil
	}

	req := c.client.R().
		SetBody(body).
		ForceContentType("application/json")
	req.SetQueryParam("node_type", c.NodeType)

	res, reqErr := req.Post(path)
	response, err := c.parseResponse(res, path, reqErr)
	if err != nil {
		// Clear any stale kick list — policy applies only after a successful report.
		c.access.Lock()
		c.lastKickUIDs = nil
		c.access.Unlock()
		return err
	}

	// Reset then parse optional policy UIDs (default empty → no user impact).
	uids := make([]int, 0)
	if response != nil {
		if kick, ok := response.CheckGet("kick_uids"); ok {
			uids = append(uids, parseUIDList(kick)...)
		}
		if dis, ok := response.CheckGet("disable_uids"); ok {
			uids = append(uids, parseUIDList(dis)...)
		}
		// De-dupe while preserving order
		if len(uids) > 1 {
			seen := make(map[int]struct{}, len(uids))
			dedup := uids[:0]
			for _, id := range uids {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				dedup = append(dedup, id)
			}
			uids = dedup
		}
	}
	c.access.Lock()
	c.lastKickUIDs = uids
	c.access.Unlock()
	return nil
}

// TakeKickUIDs returns and clears panel-requested kick/disable UIDs from the
// last successful ReportIllegal. Safe when empty (P3 policy off).
func (c *APIClient) TakeKickUIDs() []int {
	c.access.Lock()
	defer c.access.Unlock()
	out := c.lastKickUIDs
	c.lastKickUIDs = nil
	return out
}

// ParseUniProxyNodeResponse parses the UniProxy config response.
// Response format:
//
//	{
//	  "server_port": 443,
//	  "network": "tcp",
//	  "tls": 2,                     // 0=none, 1=tls, 2=reality
//	  "flow": "xtls-rprx-vision",
//	  "decryption": "none",         // optional; VLESS Encryption server string (PQ)
//	  "tls_settings": {
//	    "server_name": "...",
//	    "public_key": "...",
//	    "private_key": "...",
//	    "short_id": "...",
//	    "dest": "...",
//	    "server_port": "443",
//	    "min_client_ver": "1.8.0",  // optional; overridable by ControllerConfig.RealityMinClientVer
//	    "mldsa65_seed": "..."       // optional; REALITY ML-DSA-65 PQ signature seed
//	  },
//	  "base_config": { "push_interval": 60, "pull_interval": 60 }
//	}
func (c *APIClient) ParseUniProxyNodeResponse(response *simplejson.Json) (*api.NodeInfo, error) {
	port := response.Get("server_port").MustInt()
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid server_port %d (must be 1-65535)", port)
	}
	network := response.Get("network").MustString()
	if network == "" {
		network = "tcp"
	}

	tlsMode := response.Get("tls").MustInt() // 0=none, 1=tls, 2=reality
	flow := response.Get("flow").MustString() // "xtls-rprx-vision" or empty
	vlessDecryption := resolvePanelVlessDecryption(response)

	var enableTLS bool
	var TLSType string
	var enableVision bool
	var realitySettings json.RawMessage

	switch tlsMode {
	case 2: // REALITY
		enableTLS = true
		TLSType = "reality"
		enableVision = (flow == "xtls-rprx-vision")
		realitySettings = buildRealitySettings(response.Get("tls_settings"))
	case 1: // TLS
		enableTLS = true
		TLSType = "tls"
		enableVision = (flow == "xtls-rprx-vision")
	default: // no TLS
		enableTLS = false
		TLSType = ""
		enableVision = false
	}

	// Also check EnableReality from config as override.
	// Skip AnyTLS nodes: their TLS/REALITY mode is determined solely by the
	// panel response (tls field), not by the global EnableReality flag.
	// Forcing REALITY on an AnyTLS node configured as plain TLS would break
	// existing clients that expect standard TLS on that port.
	if c.EnableReality && tlsMode != 2 && !strings.EqualFold(c.NodeType, "anytls") {
		TLSType = "reality"
		enableVision = true
		enableTLS = true
	}

	nodeInfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              port,
		TransportProtocol: network,
		EnableTLS:         enableTLS,
		TLSType:           TLSType,
		EnableVision:      enableVision,
		EnableVless:       true, // UniProxy always VLESS
		RealitySettings:   realitySettings,
		VlessDecryption:   vlessDecryption,
	}
	return nodeInfo, nil
}

// defaultRealityMinClientVer is applied when neither the panel nor config.yml
// supplies a minClientVer. Kept deliberately low for third-party client
// compatibility (Mihomo etc. often report 1.8.x). Must stay non-empty so that
// xray-core ≥ 26.7.11 does not fall through to its built-in default 26.3.27.
const defaultRealityMinClientVer = "1.8.0"

// buildRealitySettings converts UniProxy tls_settings to REALITYConfig JSON.
// Panel-side fields (local config.yml overrides applied later in controller):
//   - min_client_ver / minClientVer  → minClientVer (default "1.8.0")
//   - mldsa65_seed / mldsa65Seed     → mldsa65Seed  (optional PQ cert signature)
//   - show                           → show         (optional debug)
//
// X25519MLKEM768 (REALITY TLS PQ key agreement) needs no field: when dest
// supports it, xray-core negotiates automatically on both ends.
func buildRealitySettings(tlsSettings *simplejson.Json) json.RawMessage {
	serverName := tlsSettings.Get("server_name").MustString()
	dest := tlsSettings.Get("dest").MustString()
	serverPort := tlsSettings.Get("server_port").MustString()
	publicKey := tlsSettings.Get("public_key").MustString()
	privateKey := tlsSettings.Get("private_key").MustString()
	shortId := tlsSettings.Get("short_id").MustString()
	fingerprint := tlsSettings.Get("fingerprint").MustString()
	minClientVer := resolvePanelMinClientVer(tlsSettings)
	mldsa65Seed := resolvePanelMldsa65Seed(tlsSettings)
	show := tlsSettings.Get("show").MustBool()

	if serverName == "" && privateKey == "" {
		return nil
	}

	// Resolve the port: fall back to "443" when the panel omits it.
	port := serverPort
	if port == "" {
		port = "443"
	}
	// When dest is empty, fall back to serverName (the SNI) as the REALITY
	// forwarding target, which is the most common deployment pattern.
	if dest == "" {
		dest = serverName
	}

	// Determine the effective destination address. net.SplitHostPort correctly
	// handles all cases: plain hostnames, IPv4 addresses, and IPv6 literals
	// ("[::1]:443"). The old hand-rolled colon scan was broken for bare IPv6
	// addresses (e.g. "2001:db8::1") because it detected the first colon in the
	// address and incorrectly concluded that a port was already present.
	destAddr := dest
	if _, _, err := net.SplitHostPort(dest); err != nil {
		// dest has no port — append the configured port.
		// net.JoinHostPort correctly brackets IPv6 addresses.
		destAddr = net.JoinHostPort(dest, port)
	}

	// Default fingerprint to "chrome" if not provided by the panel.
	if fingerprint == "" {
		fingerprint = "chrome"
	}

	realityConfig := map[string]interface{}{
		"show":         show,
		"dest":         destAddr,
		"serverNames":  []string{serverName},
		"privateKey":   privateKey,
		"publicKey":    publicKey,
		"shortIds":     []string{shortId},
		"fingerprint":  fingerprint,
		"minClientVer": minClientVer,
		"maxTimeDiff":  60000,
	}
	if mldsa65Seed != "" {
		realityConfig["mldsa65Seed"] = mldsa65Seed
	}
	raw, _ := json.Marshal(realityConfig)
	return raw
}

// resolvePanelMinClientVer reads optional minClientVer fields from panel
// tls_settings. Returns the built-in default when the panel omits them.
func resolvePanelMinClientVer(tlsSettings *simplejson.Json) string {
	if tlsSettings == nil {
		return defaultRealityMinClientVer
	}
	// Prefer snake_case (UniProxy / PHP panel convention), then camelCase.
	if v := strings.TrimSpace(tlsSettings.Get("min_client_ver").MustString()); v != "" {
		return v
	}
	if v := strings.TrimSpace(tlsSettings.Get("minClientVer").MustString()); v != "" {
		return v
	}
	return defaultRealityMinClientVer
}

// resolvePanelMldsa65Seed reads optional REALITY ML-DSA-65 seed from panel
// tls_settings. Empty means PQ cert signature is disabled.
func resolvePanelMldsa65Seed(tlsSettings *simplejson.Json) string {
	if tlsSettings == nil {
		return ""
	}
	if v := strings.TrimSpace(tlsSettings.Get("mldsa65_seed").MustString()); v != "" {
		return v
	}
	if v := strings.TrimSpace(tlsSettings.Get("mldsa65Seed").MustString()); v != "" {
		return v
	}
	return ""
}

// resolvePanelVlessDecryption reads optional VLESS Encryption server string.
// Empty means the controller will fall back to "none" (or config.yml override).
// Accepts top-level "decryption" / "vless_decryption" on the UniProxy config body.
func resolvePanelVlessDecryption(response *simplejson.Json) string {
	if response == nil {
		return ""
	}
	if v := strings.TrimSpace(response.Get("decryption").MustString()); v != "" {
		return v
	}
	if v := strings.TrimSpace(response.Get("vless_decryption").MustString()); v != "" {
		return v
	}
	return ""
}

// ParseUniProxyUserResponse parses the UniProxy user list response.
// Response format: {"users": [{"id": X, "uuid": "...", "email": "...", "speed_limit": 0}]}
func (c *APIClient) ParseUniProxyUserResponse(response *simplejson.Json) (*[]api.UserInfo, error) {
	usersArray := response.Get("users").MustArray()
	userList := make([]api.UserInfo, len(usersArray))
	for i := range usersArray {
		user := api.UserInfo{}
		user.UID = response.Get("users").GetIndex(i).Get("id").MustInt()
		user.UUID = response.Get("users").GetIndex(i).Get("uuid").MustString()
		user.Email = response.Get("users").GetIndex(i).Get("email").MustString()
		if user.Email == "" {
			user.Email = user.UUID
		}
		user.SpeedLimit = uint64(c.SpeedLimit * 1000000 / 8)
		// Per-user speed_limit from panel (Mbps → Bps), overrides global default
		if sl, ok := response.Get("users").GetIndex(i).CheckGet("speed_limit"); ok {
			if v, err := sl.Int64(); err == nil && v > 0 {
				user.SpeedLimit = uint64(v * 1000000 / 8)
			}
		}
		// Global device limit from config.yml as default; panel per-user
		// device_limit overrides when present (0 = unlimited).
		user.DeviceLimit = c.DeviceLimit
		if dl, ok := response.Get("users").GetIndex(i).CheckGet("device_limit"); ok {
			if v, err := dl.Int64(); err == nil && v >= 0 {
				user.DeviceLimit = int(v)
			}
		}
		userList[i] = user
	}
	// Summary: count users with per-user speed limits
	limited := 0
	for _, u := range userList {
		if u.SpeedLimit > 0 {
			limited++
		}
	}
	if limited > 0 {
		log.Printf("[Vless: %d] speed_limit parsed: %d/%d users have per-user limits", c.NodeID, limited, len(userList))
	} else {
		log.Printf("[Vless: %d] speed_limit: all %d users use global limit (%.0f Mbps)", c.NodeID, len(userList), c.SpeedLimit)
	}
	return &userList, nil
}
