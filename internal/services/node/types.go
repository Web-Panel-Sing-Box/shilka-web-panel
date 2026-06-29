package node

import (
	"time"

	"sing-box-web-panel/internal/domain"
)

type Input struct {
	Name                string
	Remark              string
	Scheme              string
	Address             string
	Port                int
	BasePath            string
	APITokenSecret      string
	Enabled             bool
	AllowPrivateAddress bool
	SkipTLSVerify       bool
}

type RemoteStatus struct {
	PanelVersion  string  `json:"panelVersion"`
	CoreVersion   string  `json:"coreVersion"`
	CoreStatus    string  `json:"coreStatus"`
	CPUPct        float64 `json:"cpuPct"`
	RAMPct        float64 `json:"ramPct"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
	InboundCount  int     `json:"inboundCount"`
	ClientCount   int     `json:"clientCount"`
	OnlineCount   int     `json:"onlineCount"`
	DepletedCount int     `json:"depletedCount"`
}

type RemoteInbound struct {
	ID           string                 `json:"id"`
	Remark       string                 `json:"remark"`
	Protocol     domain.Protocol        `json:"protocol"`
	Port         int                    `json:"port"`
	Transmission domain.Transmission    `json:"transmission"`
	TLS          domain.TLSMode         `json:"tls"`
	SNI          string                 `json:"sni,omitempty"`
	Dest         string                 `json:"dest,omitempty"`
	Enabled      bool                   `json:"enabled"`
	Settings     domain.InboundSettings `json:"settings"`
	UpdatedAt    string                 `json:"updatedAt,omitempty"`
}

type RemoteInboundRequest struct {
	Remark          string `json:"remark"`
	Protocol        string `json:"protocol"`
	Port            int    `json:"port"`
	Transmission    string `json:"transmission"`
	TLS             string `json:"tls"`
	SNI             string `json:"sni"`
	Dest            string `json:"dest"`
	ACMEDomain      string `json:"acmeDomain,omitempty"`
	ACMEEmail       string `json:"acmeEmail,omitempty"`
	CertPath        string `json:"certPath,omitempty"`
	KeyPath         string `json:"keyPath,omitempty"`
	AllowInsecure   *bool  `json:"allowInsecure,omitempty"`
	WSPath          string `json:"wsPath,omitempty"`
	GRPCServiceName string `json:"grpcServiceName,omitempty"`
	HTTPUpgradePath string `json:"httpUpgradePath,omitempty"`
	HTTPUpgradeHost string `json:"httpUpgradeHost,omitempty"`

	MultiplexEnabled bool `json:"multiplexEnabled,omitempty"`

	Hy2UpMbps                int    `json:"hy2UpMbps,omitempty"`
	Hy2DownMbps              int    `json:"hy2DownMbps,omitempty"`
	Hy2IgnoreClientBandwidth bool   `json:"hy2IgnoreClientBandwidth,omitempty"`
	Hy2ObfsPassword          string `json:"hy2ObfsPassword,omitempty"`
	Hy2ObfsMinPacketSize     int    `json:"hy2ObfsMinPacketSize,omitempty"`
	Hy2ObfsMaxPacketSize     int    `json:"hy2ObfsMaxPacketSize,omitempty"`
	Hy2Masquerade            string `json:"hy2Masquerade,omitempty"`
	Hy2Network               string `json:"hy2Network,omitempty"`
	Hy2BrutalDebug           bool   `json:"hy2BrutalDebug,omitempty"`
	Hy2BbrProfile            string `json:"hy2BbrProfile,omitempty"`

	NaiveNetwork            string `json:"naiveNetwork,omitempty"`
	NaiveQuicCongestionCtrl string `json:"naiveQuicCongestionCtrl,omitempty"`
}

type RemoteClient struct {
	ID                 string              `json:"id"`
	InboundID          string              `json:"inboundId"`
	Name               string              `json:"name"`
	UUID               string              `json:"uuid"`
	Password           string              `json:"password"`
	UsedUp             int64               `json:"usedUp"`
	UsedDown           int64               `json:"usedDown"`
	TotalQuota         int64               `json:"totalQuota"`
	Expiry             string              `json:"expiry,omitempty"`
	Status             domain.ClientStatus `json:"status"`
	SubToken           string              `json:"subToken"`
	StartAfterFirstUse bool                `json:"startAfterFirstUse"`
	Enabled            bool                `json:"enabled"`
	FirstUsedAt        string              `json:"firstUsedAt,omitempty"`
}

type RemoteClientCreateRequest struct {
	Name               string `json:"name"`
	InboundID          string `json:"inboundId"`
	TotalQuota         int64  `json:"totalQuota"`
	Expiry             string `json:"expiry,omitempty"`
	StartAfterFirstUse bool   `json:"startAfterFirstUse"`
}

type RemoteClientUpdateRequest struct {
	Name               *string `json:"name,omitempty"`
	InboundID          *string `json:"inboundId,omitempty"`
	TotalQuota         *int64  `json:"totalQuota,omitempty"`
	Expiry             *string `json:"expiry,omitempty"`
	Status             *string `json:"status,omitempty"`
	StartAfterFirstUse *bool   `json:"startAfterFirstUse,omitempty"`
}

type RemoteClientStatusRequest struct {
	Status string `json:"status"`
}

type RemoteClientBulkRequest struct {
	IDs    []string `json:"ids"`
	Status string   `json:"status,omitempty"`
}

type RemoteClientBulkResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type RemoteClientBulkResponse struct {
	Results []RemoteClientBulkResult `json:"results"`
}

type RemoteSnapshot struct {
	Status   RemoteStatus    `json:"status"`
	Inbounds []RemoteInbound `json:"inbounds"`
	Clients  []RemoteClient  `json:"clients"`
}

type SyncResult struct {
	NodeID       int64
	InboundCount int
	ClientCount  int
	SyncedAt     time.Time
}
