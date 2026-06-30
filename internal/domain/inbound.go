package domain

import "time"

// Protocol is the inbound proxy protocol. v1 supports the set modelled by the
// frontend: VLESS, Naive and Hysteria2.
type Protocol string

const (
	ProtocolVLESS     Protocol = "vless"
	ProtocolNaive     Protocol = "naive"
	ProtocolHysteria2 Protocol = "hysteria2"
)

// Transmission is the stream transport for VLESS. Naive and Hysteria2 always
// use their own transport and ignore this field.
type Transmission string

const (
	TransmissionTCP         Transmission = "tcp"
	TransmissionWS          Transmission = "ws"
	TransmissionGRPC        Transmission = "grpc"
	TransmissionHTTPUpgrade Transmission = "httpupgrade"
)

// TLSMode selects the security layer applied to an inbound.
type TLSMode string

const (
	TLSModeNone    TLSMode = "none"
	TLSModeTLS     TLSMode = "tls"
	TLSModeReality TLSMode = "reality"
)

// Inbound is a configured sing-box listener. Protocol-specific and rarely
// changing details live in Settings (persisted as JSON).
type Inbound struct {
	ID            int64
	NodeID        *int64
	RemoteID      string
	RemoteVersion string
	Remark        string
	Protocol      Protocol
	Port          int
	Transmission  Transmission
	TLS           TLSMode
	SNI           string
	Dest          string
	Enabled       bool
	Settings      InboundSettings
	LastSyncedAt  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// InboundSettings holds protocol/transport/TLS material that is generated or
// configured once and reused on every config render. The Reality private key
// is a secret and must never be logged or returned by the public API.
type InboundSettings struct {
	// Reality (VLESS).
	RealityPrivateKey string `json:"realityPrivateKey,omitempty"`
	RealityPublicKey  string `json:"realityPublicKey,omitempty"`
	RealityShortID    string `json:"realityShortId,omitempty"`
	// Flow for VLESS (e.g. "xtls-rprx-vision" with Reality).
	Flow string `json:"flow,omitempty"`
	// Transport options (VLESS WS/gRPC/HTTPUpgrade).
	WSPath          string `json:"wsPath,omitempty"`
	GRPCServiceName string `json:"grpcServiceName,omitempty"`
	HTTPUpgradePath string `json:"httpUpgradePath,omitempty"`
	HTTPUpgradeHost string `json:"httpUpgradeHost,omitempty"`
	// Standard TLS certificate material (mode = tls, no ACME).
	CertPath string `json:"certPath,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	// ACME (Let's Encrypt) for the inbound, handled by the sing-box core.
	ACMEDomain string `json:"acmeDomain,omitempty"`
	ACMEEmail  string `json:"acmeEmail,omitempty"`
	// Client-side TLS verification override for generated links/subscriptions.
	// nil means automatic: allow insecure for TLS inbounds without trusted cert material.
	AllowInsecure *bool `json:"allowInsecure,omitempty"`

	// Hysteria2.
	Hy2UpMbps                int    `json:"hy2UpMbps,omitempty"`
	Hy2DownMbps              int    `json:"hy2DownMbps,omitempty"`
	Hy2IgnoreClientBandwidth bool   `json:"hy2IgnoreClientBandwidth,omitempty"`
	Hy2ObfsPassword          string `json:"hy2ObfsPassword,omitempty"`
	Hy2ObfsMinPacketSize     int    `json:"hy2ObfsMinPacketSize,omitempty"` // gecko only
	Hy2ObfsMaxPacketSize     int    `json:"hy2ObfsMaxPacketSize,omitempty"` // gecko only
	Hy2Masquerade            string `json:"hy2Masquerade,omitempty"`
	Hy2Network               string `json:"hy2Network,omitempty"` // tcp, udp, ""
	Hy2BrutalDebug           bool   `json:"hy2BrutalDebug,omitempty"`
	Hy2BbrProfile            string `json:"hy2BbrProfile,omitempty"` // conservative, standard, aggressive

	// Naive.
	NaiveNetwork            string `json:"naiveNetwork,omitempty"`            // tcp, udp, ""
	NaiveQuicCongestionCtrl string `json:"naiveQuicCongestionCtrl,omitempty"` // bbr, cubic, reno, etc.

	// VLESS multiplex.
	MultiplexEnabled bool `json:"multiplexEnabled,omitempty"`
}

// EffectiveAllowInsecure reports whether generated client subscription output
// should skip server certificate verification for this inbound.
func (ib *Inbound) EffectiveAllowInsecure() bool {
	if ib == nil || ib.TLS != TLSModeTLS {
		return false
	}
	if ib.Settings.AllowInsecure != nil {
		return *ib.Settings.AllowInsecure
	}
	return ib.Settings.ACMEDomain == "" && ib.Settings.CertPath == "" && ib.Settings.KeyPath == ""
}
