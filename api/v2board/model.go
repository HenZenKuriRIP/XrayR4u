package v2board

// UserTraffic is the traffic report format expected by K2Board/UniProxy.
// Format: {user_id: [upload, download]}
type UserTraffic struct {
	UID      int   `json:"user_id"`
	Upload   int64 `json:"u"`
	Download int64 `json:"d"`
}

// NodeStatus is the system status payload sent to the UniProxy node status endpoint.
type NodeStatus struct {
	CPU         float64 `json:"cpu"`
	Mem         float64 `json:"mem"`
	Disk        float64 `json:"disk"`
	Uptime      int     `json:"uptime"`
	ActiveConns int     `json:"active_conns"`
}
