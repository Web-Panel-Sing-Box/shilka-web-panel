package sublink

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"sing-box-web-panel/internal/domain"
)

// ErrNaiveJSONRequiresTrustedTLS is returned when a Naive sing-box JSON
// subscription would need client-side insecure TLS. sing-box does not support
// the insecure TLS option on naive outbounds.
var ErrNaiveJSONRequiresTrustedTLS = errors.New("naive json subscription requires trusted TLS; use ACME/custom cert or plain/base64 link")

// Supported subscription output formats.
const (
	FormatPlain  = "plain"
	FormatBase64 = "base64"
	FormatJSON   = "json"
)

// Result is a rendered subscription payload.
type Result struct {
	ContentType string
	Body        []byte
}

// Render builds a subscription payload for a single client/inbound pair in the
// requested format. Unknown formats default to base64.
func Render(format string, ib *domain.Inbound, c *domain.Client, host string) (Result, error) {
	switch format {
	case FormatPlain:
		return Result{ContentType: "text/plain; charset=utf-8", Body: []byte(BuildLink(ib, c, host))}, nil
	case FormatJSON:
		body, err := BuildClientConfig(ib, c, host)
		if err != nil {
			return Result{}, err
		}
		return Result{ContentType: "application/json", Body: body}, nil
	default: // base64
		link := BuildLink(ib, c, host)
		enc := base64.StdEncoding.EncodeToString([]byte(link))
		return Result{ContentType: "text/plain; charset=utf-8", Body: []byte(enc)}, nil
	}
}

// RenderMulti builds a subscription payload covering every inbound the client is
// bound to (SIN-11). plain/base64 join one share link per inbound; json emits one
// proxy outbound per inbound behind a "proxy" selector. Falls back to single-link
// behaviour when only one inbound is given. Unknown formats default to base64.
func RenderMulti(format string, inbounds []*domain.Inbound, c *domain.Client, host string) (Result, error) {
	switch format {
	case FormatJSON:
		body, err := BuildClientConfigMulti(inbounds, c, host)
		if err != nil {
			return Result{}, err
		}
		return Result{ContentType: "application/json", Body: body}, nil
	case FormatPlain:
		return Result{ContentType: "text/plain; charset=utf-8", Body: []byte(joinLinks(inbounds, c, host))}, nil
	default: // base64
		enc := base64.StdEncoding.EncodeToString([]byte(joinLinks(inbounds, c, host)))
		return Result{ContentType: "text/plain; charset=utf-8", Body: []byte(enc)}, nil
	}
}

func joinLinks(inbounds []*domain.Inbound, c *domain.Client, host string) string {
	links := make([]string, 0, len(inbounds))
	for _, ib := range inbounds {
		if l := BuildLink(ib, c, host); l != "" {
			links = append(links, l)
		}
	}
	return strings.Join(links, "\n")
}

// BuildClientConfigMulti renders a sing-box client config with one proxy outbound
// per inbound, all selectable via a "proxy" selector that route.final points at.
// Inbounds that cannot produce a valid outbound (e.g. naive with insecure TLS)
// are skipped; if none remain, the skip reason is returned.
func BuildClientConfigMulti(inbounds []*domain.Inbound, c *domain.Client, host string) ([]byte, error) {
	var (
		outbounds []any
		tags      []string
		skipErr   error
	)
	for i, ib := range inbounds {
		tag := fmt.Sprintf("proxy-%d", i+1)
		out, err := buildProxyOutboundTagged(ib, c, host, tag)
		if err != nil {
			if errors.Is(err, ErrNaiveJSONRequiresTrustedTLS) {
				if skipErr == nil {
					skipErr = err
				}
				continue
			}
			return nil, err
		}
		outbounds = append(outbounds, out)
		tags = append(tags, tag)
	}
	if len(outbounds) == 0 {
		if skipErr != nil {
			return nil, skipErr
		}
		return nil, fmt.Errorf("no usable inbound for client config")
	}
	all := make([]any, 0, len(outbounds)+2)
	all = append(all, map[string]any{"type": "selector", "tag": "proxy", "outbounds": tags})
	all = append(all, outbounds...)
	all = append(all, map[string]any{"type": "direct", "tag": "direct"})
	cfg := clientConfig{
		Log:       map[string]any{"level": "info"},
		Outbounds: all,
		Route:     map[string]any{"final": "proxy"},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// --- sing-box client config (JSON format) ---

type clientConfig struct {
	Log       map[string]any `json:"log,omitempty"`
	Outbounds []any          `json:"outbounds"`
	Route     map[string]any `json:"route,omitempty"`
}

type clientOutTLS struct {
	Enabled    bool              `json:"enabled"`
	ServerName string            `json:"server_name,omitempty"`
	Insecure   bool              `json:"insecure,omitempty"`
	ALPN       []string          `json:"alpn,omitempty"`
	Reality    *clientOutReality `json:"reality,omitempty"`
	UTLS       *clientOutUTLS    `json:"utls,omitempty"`
}

type clientOutReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
}

type clientOutUTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type clientOutTransport struct {
	Type        string `json:"type"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}

type vlessOutbound struct {
	Type       string              `json:"type"`
	Tag        string              `json:"tag"`
	Server     string              `json:"server"`
	ServerPort int                 `json:"server_port"`
	UUID       string              `json:"uuid"`
	Flow       string              `json:"flow,omitempty"`
	TLS        *clientOutTLS       `json:"tls,omitempty"`
	Transport  *clientOutTransport `json:"transport,omitempty"`
}

type hysteria2Outbound struct {
	Type        string               `json:"type"`
	Tag         string               `json:"tag"`
	Server      string               `json:"server"`
	ServerPort  int                  `json:"server_port"`
	Password    string               `json:"password"`
	UpMbps      int                  `json:"up_mbps,omitempty"`
	DownMbps    int                  `json:"down_mbps,omitempty"`
	Network     string               `json:"network,omitempty"`
	Obfs        *clientHysteria2Obfs `json:"obfs,omitempty"`
	BBRProfile  string               `json:"bbr_profile,omitempty"`
	BrutalDebug bool                 `json:"brutal_debug,omitempty"`
	TLS         *clientOutTLS        `json:"tls,omitempty"`
}

type clientHysteria2Obfs struct {
	Type          string `json:"type"`
	Password      string `json:"password"`
	MinPacketSize int    `json:"min_packet_size,omitempty"`
	MaxPacketSize int    `json:"max_packet_size,omitempty"`
}

type naiveOutbound struct {
	Type       string        `json:"type"`
	Tag        string        `json:"tag"`
	Server     string        `json:"server"`
	ServerPort int           `json:"server_port"`
	Username   string        `json:"username"`
	Password   string        `json:"password"`
	TLS        *clientOutTLS `json:"tls,omitempty"`
}

// BuildClientConfig renders a minimal sing-box client config whose proxy
// outbound mirrors the inbound. Modern clients import this directly.
func BuildClientConfig(ib *domain.Inbound, c *domain.Client, host string) ([]byte, error) {
	proxy, err := buildProxyOutbound(ib, c, host)
	if err != nil {
		return nil, err
	}
	cfg := clientConfig{
		Log: map[string]any{"level": "info"},
		Outbounds: []any{
			proxy,
			map[string]any{"type": "direct", "tag": "direct"},
		},
		Route: map[string]any{"final": "proxy"},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func buildProxyOutbound(ib *domain.Inbound, c *domain.Client, host string) (any, error) {
	return buildProxyOutboundTagged(ib, c, host, "proxy")
}

func buildProxyOutboundTagged(ib *domain.Inbound, c *domain.Client, host, tag string) (any, error) {
	switch ib.Protocol {
	case domain.ProtocolVLESS:
		out := vlessOutbound{
			Type: "vless", Tag: tag, Server: host, ServerPort: ib.Port,
			UUID: c.UUID, Flow: ib.Settings.Flow,
			TLS:       clientTLS(ib),
			Transport: clientTransport(ib),
		}
		return out, nil
	case domain.ProtocolHysteria2:
		out := hysteria2Outbound{
			Type: "hysteria2", Tag: tag, Server: host, ServerPort: ib.Port,
			Password: c.Password, TLS: clientTLS(ib),
			UpMbps: ib.Settings.Hy2UpMbps, DownMbps: ib.Settings.Hy2DownMbps,
			Network: ib.Settings.Hy2Network, BBRProfile: ib.Settings.Hy2BbrProfile,
			BrutalDebug: ib.Settings.Hy2BrutalDebug,
		}
		if ib.Settings.Hy2ObfsPassword != "" {
			out.Obfs = &clientHysteria2Obfs{
				Type:          "salamander",
				Password:      ib.Settings.Hy2ObfsPassword,
				MinPacketSize: ib.Settings.Hy2ObfsMinPacketSize,
				MaxPacketSize: ib.Settings.Hy2ObfsMaxPacketSize,
			}
		}
		return out, nil
	case domain.ProtocolNaive:
		if ib.EffectiveAllowInsecure() {
			return nil, ErrNaiveJSONRequiresTrustedTLS
		}
		return naiveOutbound{
			Type: "naive", Tag: tag, Server: host, ServerPort: ib.Port,
			Username: c.Name, Password: c.Password, TLS: naiveClientTLS(ib),
		}, nil
	default:
		return map[string]any{"type": "direct", "tag": tag}, nil
	}
}

func clientTLS(ib *domain.Inbound) *clientOutTLS {
	switch ib.TLS {
	case domain.TLSModeReality:
		return &clientOutTLS{
			Enabled:    true,
			ServerName: ib.SNI,
			UTLS:       &clientOutUTLS{Enabled: true, Fingerprint: "chrome"},
			Reality: &clientOutReality{
				Enabled:   true,
				PublicKey: ib.Settings.RealityPublicKey,
				ShortID:   ib.Settings.RealityShortID,
			},
		}
	case domain.TLSModeTLS:
		tls := &clientOutTLS{Enabled: true, ServerName: ib.SNI, Insecure: ib.EffectiveAllowInsecure()}
		if ib.Protocol == domain.ProtocolHysteria2 {
			tls.ALPN = []string{"h3"}
		}
		return tls
	default:
		return nil
	}
}

func naiveClientTLS(ib *domain.Inbound) *clientOutTLS {
	if ib.TLS != domain.TLSModeTLS {
		return nil
	}
	return &clientOutTLS{Enabled: true, ServerName: ib.SNI}
}

func clientTransport(ib *domain.Inbound) *clientOutTransport {
	switch ib.Transmission {
	case domain.TransmissionWS:
		return &clientOutTransport{Type: "ws", Path: ib.Settings.WSPath}
	case domain.TransmissionGRPC:
		return &clientOutTransport{Type: "grpc", ServiceName: ib.Settings.GRPCServiceName}
	case domain.TransmissionHTTPUpgrade:
		return &clientOutTransport{
			Type: "httpupgrade",
			Host: ib.Settings.HTTPUpgradeHost,
			Path: ib.Settings.HTTPUpgradePath,
		}
	default:
		return nil
	}
}

// HostFromRequestHost strips an optional port from a Host header value.
func HostFromRequestHost(hostHeader string) string {
	if i := strings.LastIndex(hostHeader, ":"); i > 0 && !strings.Contains(hostHeader[i:], "]") {
		return hostHeader[:i]
	}
	return hostHeader
}
