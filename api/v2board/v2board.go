package v2board

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/api"
	"github.com/HenZenKuriRIP/XrayR4u/common/regexutil"
	"github.com/bitly/go-simplejson"
	"github.com/go-resty/resty/v2"
)

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
			// v.Response contains the last response from the server
			// v.Err contains the original error
			log.Print(v.Err)
		}
	})
	client.SetBaseURL(apiConfig.APIHost)
	// Create Key for each requests
	client.SetQueryParams(map[string]string{
		"node_id": strconv.Itoa(apiConfig.NodeID),
		"token":   apiConfig.Key,
	})
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
	if path != "" {
		// open the file
		file, err := os.Open(path)

		//handle errors while opening
		if err != nil {
			log.Printf("Error when opening file: %s", err)
			return LocalRuleList
		}

		fileScanner := bufio.NewScanner(file)

		// read line by line
		for fileScanner.Scan() {
			LocalRuleList = append(LocalRuleList, api.DetectRule{
				ID:      -1,
				Pattern: regexutil.SafeCompileOrDefault(fileScanner.Text()),
			})
		}
		// handle first encountered error while reading
		if err := fileScanner.Err(); err != nil {
			log.Fatalf("Error while reading file: %s", err)
			return make([]api.DetectRule, 0)
		}

		file.Close()
	}

	return LocalRuleList
}

// Describe return a description of the client
func (c *APIClient) Describe() api.ClientInfo {
	return api.ClientInfo{APIHost: c.APIHost, NodeID: c.NodeID, Key: c.Key, NodeType: c.NodeType}
}

// Debug set the client debug for client
func (c *APIClient) Debug() {
	c.client.SetDebug(true)
}

func (c *APIClient) assembleURL(path string) string {
	return c.APIHost + path
}

// parseResponse validates and decodes a resty response.
// Passing the reqErr from the resty call as err allows the function to be the
// single point of error handling: if the request itself failed, or the
// response is nil or carries a ≥ 400 status, an error is returned.
func (c *APIClient) parseResponse(res *resty.Response, path string, err error) (*simplejson.Json, error) {
	if err != nil {
		return nil, fmt.Errorf("request %s failed: %w", c.assembleURL(path), err)
	}
	if res == nil {
		return nil, fmt.Errorf("request %s returned nil response", c.assembleURL(path))
	}
	if res.StatusCode() >= 400 {
		body := res.Body()
		return nil, fmt.Errorf("request %s failed: %s", c.assembleURL(path), string(body))
	}
	rtn, err := simplejson.NewJson(res.Body())
	if err != nil {
		return nil, fmt.Errorf("Ret %s invalid", res.String())
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
		res, _ := response.MarshalJSON()
		return nil, fmt.Errorf("Parse node info failed: %s, \nError: %s", string(res), err)
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

// GetNodeRule implements the API interface
func (c *APIClient) GetNodeRule() (*[]api.DetectRule, error) {
	ruleList := c.LocalRuleList

	// UniProxy returns rule info in the config response under "routes"
	c.access.Lock()
	defer c.access.Unlock()
	if c.ConfigResp != nil {
		routes := c.ConfigResp.Get("routes")
		if rulesArray, ok := routes.CheckGet("rules"); ok {
			for i := range rulesArray.MustArray() {
				rule := rulesArray.GetIndex(i)
				if domain, ok := rule.CheckGet("domain"); ok {
					for _, d := range domain.MustStringArray() {
						ruleListItem := api.DetectRule{
							ID:      i,
							Pattern: regexutil.SafeCompileOrDefault(d),
						}
						ruleList = append(ruleList, ruleListItem)
					}
				}
			}
		}
	}
	return &ruleList, nil
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

// ReportIllegal implements the API interface
func (c *APIClient) ReportIllegal(detectResultList *[]api.DetectResult) error {
	return nil
}

// ParseUniProxyNodeResponse parses the UniProxy config response.
// Response format:
//
//	{
//	  "server_port": 443,
//	  "network": "tcp",
//	  "tls": 2,                     // 0=none, 1=tls, 2=reality
//	  "flow": "xtls-rprx-vision",
//	  "tls_settings": {
//	    "server_name": "...",
//	    "public_key": "...",
//	    "private_key": "...",
//	    "short_id": "...",
//	    "dest": "...",
//	    "server_port": "443"
//	  },
//	  "base_config": { "push_interval": 60, "pull_interval": 60 }
//	}
func (c *APIClient) ParseUniProxyNodeResponse(response *simplejson.Json) (*api.NodeInfo, error) {
	port := response.Get("server_port").MustInt()
	network := response.Get("network").MustString()
	if network == "" {
		network = "tcp"
	}

	tlsMode := response.Get("tls").MustInt() // 0=none, 1=tls, 2=reality
	flow := response.Get("flow").MustString() // "xtls-rprx-vision" or empty

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
	}
	return nodeInfo, nil
}

// buildRealitySettings converts UniProxy tls_settings to REALITYConfig JSON
func buildRealitySettings(tlsSettings *simplejson.Json) json.RawMessage {
	serverName := tlsSettings.Get("server_name").MustString()
	dest := tlsSettings.Get("dest").MustString()
	serverPort := tlsSettings.Get("server_port").MustString()
	publicKey := tlsSettings.Get("public_key").MustString()
	privateKey := tlsSettings.Get("private_key").MustString()
	shortId := tlsSettings.Get("short_id").MustString()
	fingerprint := tlsSettings.Get("fingerprint").MustString()

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
		"show":         false,
		"dest":         destAddr,
		"serverNames":  []string{serverName},
		"privateKey":   privateKey,
		"publicKey":    publicKey,
		"shortIds":     []string{shortId},
		"fingerprint":  fingerprint,
		"minClientVer": "1.8.0",
		"maxTimeDiff":  60000,
	}
	raw, _ := json.Marshal(realityConfig)
	return raw
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
