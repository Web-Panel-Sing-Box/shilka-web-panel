package singbox

// Minimal sing-box configuration schema. Only the fields the panel emits are
// modelled; JSON tags match the sing-box option structs. Targets the widely
// deployed 1.10/1.11 schema (direct/block outbounds, classic route rules).

type sbConfig struct {
	Log          *sbLog          `json:"log,omitempty"`
	Inbounds     []any           `json:"inbounds,omitempty"`
	Outbounds    []sbOutbound    `json:"outbounds,omitempty"`
	Route        *sbRoute        `json:"route,omitempty"`
	Experimental *sbExperimental `json:"experimental,omitempty"`
}

type sbLog struct {
	Level     string `json:"level,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
	Output    string `json:"output,omitempty"`
}

type sbOutbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type sbRoute struct {
	Rules []sbRouteRule `json:"rules,omitempty"`
	Final string        `json:"final,omitempty"`
}

// sbRouteRule uses rule actions (sing-box 1.11+) rather than a block outbound,
// keeping the config valid through sing-box 1.14 where block/dns outbounds and
// the legacy DNS server format were removed. `AuthUser` + `Outbound` carry the
// per-client routing that lets the panel attribute /connections.chains[0] back
// to a single client.
//
// Note: the matcher MUST be `auth_user` (inbound auth identity), not `user`
// (which sing-box ties to the OS process owner — see route/rule/rule_item_user.go).
type sbRouteRule struct {
	Protocol string   `json:"protocol,omitempty"`
	Action   string   `json:"action,omitempty"`
	AuthUser []string `json:"auth_user,omitempty"`
	Outbound string   `json:"outbound,omitempty"`
}

type sbExperimental struct {
	CacheFile *sbCacheFile `json:"cache_file,omitempty"`
	ClashAPI  *sbClashAPI  `json:"clash_api,omitempty"`
	V2RayAPI  *sbV2RayAPI  `json:"v2ray_api,omitempty"`
}

type sbCacheFile struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
}

type sbClashAPI struct {
	ExternalController string `json:"external_controller,omitempty"`
	Secret             string `json:"secret,omitempty"`
}

type sbV2RayAPI struct {
	Listen string        `json:"listen,omitempty"`
	Stats  *sbV2RayStats `json:"stats,omitempty"`
}

type sbV2RayStats struct {
	Enabled   bool     `json:"enabled"`
	Inbounds  []string `json:"inbounds,omitempty"`
	Outbounds []string `json:"outbounds,omitempty"`
	Users     []string `json:"users,omitempty"`
}

// --- inbounds ---

type sbVLESSInbound struct {
	Type       string        `json:"type"`
	Tag        string        `json:"tag"`
	Listen     string        `json:"listen"`
	ListenPort int           `json:"listen_port"`
	Users      []sbVLESSUser `json:"users"`
	TLS        *sbInboundTLS `json:"tls,omitempty"`
	Transport  *sbTransport  `json:"transport,omitempty"`
	Multiplex  *sbMultiplex  `json:"multiplex,omitempty"`
}

type sbMultiplex struct {
	Enabled bool `json:"enabled"`
}

type sbVLESSUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow,omitempty"`
}

type sbHysteria2Inbound struct {
	Type       string            `json:"type"`
	Tag        string            `json:"tag"`
	Listen     string            `json:"listen"`
	ListenPort int               `json:"listen_port"`
	Users      []sbHysteria2User `json:"users"`
	TLS        *sbInboundTLS     `json:"tls,omitempty"`

	UpMbps                int              `json:"up_mbps,omitempty"`
	DownMbps              int              `json:"down_mbps,omitempty"`
	IgnoreClientBandwidth bool             `json:"ignore_client_bandwidth,omitempty"`
	Obfs                  *sbHysteria2Obfs `json:"obfs,omitempty"`
	Masquerade            any              `json:"masquerade,omitempty"`
	Network               string           `json:"network,omitempty"`
	BrutalDebug           bool             `json:"brutal_debug,omitempty"`
	BBRProfile            string           `json:"bbr_profile,omitempty"`
}

type sbHysteria2Obfs struct {
	Type          string `json:"type"`
	Password      string `json:"password"`
	MinPacketSize int    `json:"min_packet_size,omitempty"`
	MaxPacketSize int    `json:"max_packet_size,omitempty"`
}

type sbHysteria2User struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type sbNaiveInbound struct {
	Type       string        `json:"type"`
	Tag        string        `json:"tag"`
	Listen     string        `json:"listen"`
	ListenPort int           `json:"listen_port"`
	Users      []sbNaiveUser `json:"users"`
	TLS        *sbInboundTLS `json:"tls"`

	Network               string `json:"network,omitempty"`
	QuicCongestionControl string `json:"quic_congestion_control,omitempty"`
}

type sbNaiveUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sbInboundTLS struct {
	Enabled    bool       `json:"enabled"`
	ServerName string     `json:"server_name,omitempty"`
	ALPN       []string   `json:"alpn,omitempty"`
	CertPath   string     `json:"certificate_path,omitempty"`
	KeyPath    string     `json:"key_path,omitempty"`
	ACME       *sbACME    `json:"acme,omitempty"`
	Reality    *sbReality `json:"reality,omitempty"`
}

type sbACME struct {
	Domain []string `json:"domain,omitempty"`
	Email  string   `json:"email,omitempty"`
}

type sbReality struct {
	Enabled    bool        `json:"enabled"`
	Handshake  sbHandshake `json:"handshake"`
	PrivateKey string      `json:"private_key"`
	ShortID    []string    `json:"short_id"`
}

type sbHandshake struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}

type sbTransport struct {
	Type        string `json:"type"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}
