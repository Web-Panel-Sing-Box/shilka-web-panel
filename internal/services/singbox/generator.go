package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"

	"sing-box-web-panel/internal/domain"
)

// InboundLister yields the inbounds that should appear in the live config.
type InboundLister interface {
	ListEnabled(ctx context.Context) ([]domain.Inbound, error)
}

// ClientLister yields the clients eligible to be emitted as users.
type ClientLister interface {
	ListEnabled(ctx context.Context) ([]domain.Client, error)
}

// SettingReader provides on-demand access to panel settings that affect config
// generation (e.g. log_level). It is read during every Render call so that
// runtime setting changes take effect without restarting the generator.
type SettingReader interface {
	Get(ctx context.Context, key string) (string, error)
}

// GeneratorConfig holds the static, rarely-changing knobs for config rendering.
type GeneratorConfig struct {
	LogLevel        string
	InboundListen   string // address inbounds bind to (e.g. "::")
	ClashAPIAddress string
	ClashAPISecret  string
	CacheFilePath   string
	StatsSource     string // "auto" | "clash" | "v2ray"
	V2RayAPIListen  string
	CoreLogPath     string // sing-box log output file path

	// DefaultTLSCertPath/DefaultTLSKeyPath point at a panel-managed self-signed
	// keypair used as the fallback certificate source for tls-mode inbounds that
	// have neither ACME nor explicit cert/key configured. Without this fallback
	// such inbounds emit empty certificate_path/key_path and sing-box can't bring
	// up TLS (SIN-52). Empty when the boot-time ensure failed.
	DefaultTLSCertPath string
	DefaultTLSKeyPath  string

	// Settings are the database-backed panel settings used to override hardcoded
	// defaults at render time. When nil, static GeneratorConfig fields are used.
	Settings SettingReader
}

type Generator struct {
	inbounds InboundLister
	clients  ClientLister
	cfg      GeneratorConfig
}

func NewGenerator(inbounds InboundLister, clients ClientLister, cfg GeneratorConfig) *Generator {
	if cfg.InboundListen == "" {
		cfg.InboundListen = "::"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return &Generator{inbounds: inbounds, clients: clients, cfg: cfg}
}

// Render builds the full sing-box config.json from the database.
func (g *Generator) Render(ctx context.Context) ([]byte, error) {
	inbounds, err := g.inbounds.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list inbounds: %w", err)
	}
	clients, err := g.clients.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}

	// A local client may bind to several inbounds (SIN-11): emit it into every
	// inbound block it belongs to. Fall back to the legacy single InboundID when
	// the join-table set is empty (e.g. remote clients).
	byInbound := make(map[int64][]domain.Client)
	for i := range clients {
		c := clients[i]
		if c.Status != domain.ClientStatusActive {
			continue
		}
		binds := c.InboundIDs
		if len(binds) == 0 {
			binds = []int64{c.InboundID}
		}
		for _, ibID := range binds {
			byInbound[ibID] = append(byInbound[ibID], c)
		}
	}

	var (
		built            []any
		statsUsers       []string
		perUserOutbounds []sbOutbound
		perUserRules     []sbRouteRule
	)
	// seenUser de-dupes per-user stats/outbounds/rules: a multi-inbound client
	// appears in several inbound member lists but must emit its direct outbound
	// and route rule exactly once.
	seenUser := make(map[int64]bool)
	for i := range inbounds {
		ib := inbounds[i]
		members := byInbound[ib.ID]
		entry, err := g.buildInbound(&ib, members)
		if err != nil {
			return nil, fmt.Errorf("build inbound %d: %w", ib.ID, err)
		}
		built = append(built, entry)
		for _, c := range members {
			if seenUser[c.ID] {
				continue
			}
			seenUser[c.ID] = true
			statsUsers = append(statsUsers, c.Name)
			tag := ClientOutboundTag(c.ID)
			perUserOutbounds = append(perUserOutbounds, sbOutbound{Type: "direct", Tag: tag})
			perUserRules = append(perUserRules, sbRouteRule{AuthUser: []string{c.Name}, Outbound: tag})
		}
	}

	// Resolve log level: DB setting takes precedence, then config struct, then default.
	logLevel := g.cfg.LogLevel
	if g.cfg.Settings != nil {
		if v, err := g.cfg.Settings.Get(ctx, domain.SettingLogLevel); err == nil && v != "" {
			logLevel = v
		}
	}
	if logLevel == "" {
		logLevel = "info"
	}

	// Note: the DNS block is intentionally omitted. No single DNS form is valid
	// across sing-box 1.11 (legacy only) and 1.14 (new format only), so the core
	// default is used. Blocking uses the 1.11+ "reject" rule action rather than a
	// block outbound (removed in 1.14).
	// Bittorrent reject stays first so it can't be bypassed by a per-user route.
	// Per-user rules pin every client's traffic to a unique direct outbound so
	// /connections.chains[0] is a stable client identifier.
	outbounds := append([]sbOutbound{{Type: "direct", Tag: "direct"}}, perUserOutbounds...)
	routeRules := append([]sbRouteRule{{Protocol: "bittorrent", Action: "reject"}}, perUserRules...)

	cfg := &sbConfig{
		Log:       &sbLog{Level: logLevel, Timestamp: true, Output: g.cfg.CoreLogPath},
		Inbounds:  built,
		Outbounds: outbounds,
		Route: &sbRoute{
			Rules: routeRules,
			Final: "direct",
		},
		Experimental: g.buildExperimental(statsUsers),
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return out, nil
}

func (g *Generator) buildExperimental(statsUsers []string) *sbExperimental {
	exp := &sbExperimental{
		ClashAPI: &sbClashAPI{
			ExternalController: g.cfg.ClashAPIAddress,
			Secret:             g.cfg.ClashAPISecret,
		},
	}
	if g.cfg.CacheFilePath != "" {
		exp.CacheFile = &sbCacheFile{Enabled: true, Path: g.cfg.CacheFilePath}
	}
	// Only emit v2ray_api when explicitly selected; the official binary lacks it
	// and "auto" must not break `sing-box check`.
	if g.cfg.StatsSource == "v2ray" && g.cfg.V2RayAPIListen != "" {
		exp.V2RayAPI = &sbV2RayAPI{
			Listen: g.cfg.V2RayAPIListen,
			Stats:  &sbV2RayStats{Enabled: true, Users: statsUsers},
		}
	}
	return exp
}

func (g *Generator) buildInbound(ib *domain.Inbound, clients []domain.Client) (any, error) {
	tag := inboundTag(ib)
	switch ib.Protocol {
	case domain.ProtocolVLESS:
		users := make([]sbVLESSUser, 0, len(clients))
		for _, c := range clients {
			users = append(users, sbVLESSUser{Name: c.Name, UUID: c.UUID, Flow: ib.Settings.Flow})
		}
		entry := sbVLESSInbound{
			Type:       "vless",
			Tag:        tag,
			Listen:     g.cfg.InboundListen,
			ListenPort: ib.Port,
			Users:      users,
			TLS:        g.buildTLS(ib, nil),
			Transport:  buildTransport(ib),
		}
		if ib.Settings.MultiplexEnabled {
			entry.Multiplex = &sbMultiplex{Enabled: true}
		}
		return entry, nil

	case domain.ProtocolHysteria2:
		users := make([]sbHysteria2User, 0, len(clients))
		for _, c := range clients {
			users = append(users, sbHysteria2User{Name: c.Name, Password: c.Password})
		}
		hy2 := sbHysteria2Inbound{
			Type:                  "hysteria2",
			Tag:                   tag,
			Listen:                g.cfg.InboundListen,
			ListenPort:            ib.Port,
			Users:                 users,
			TLS:                   g.buildTLS(ib, []string{"h3"}),
			UpMbps:                ib.Settings.Hy2UpMbps,
			DownMbps:              ib.Settings.Hy2DownMbps,
			IgnoreClientBandwidth: ib.Settings.Hy2IgnoreClientBandwidth,
			Network:               ib.Settings.Hy2Network,
			BrutalDebug:           ib.Settings.Hy2BrutalDebug,
			BBRProfile:            ib.Settings.Hy2BbrProfile,
		}
		if ib.Settings.Hy2ObfsPassword != "" {
			hy2.Obfs = &sbHysteria2Obfs{
				Type:          "salamander",
				Password:      ib.Settings.Hy2ObfsPassword,
				MinPacketSize: ib.Settings.Hy2ObfsMinPacketSize,
				MaxPacketSize: ib.Settings.Hy2ObfsMaxPacketSize,
			}
		}
		if ib.Settings.Hy2Masquerade != "" {
			hy2.Masquerade = ib.Settings.Hy2Masquerade
		}
		return hy2, nil

	case domain.ProtocolNaive:
		users := make([]sbNaiveUser, 0, len(clients))
		for _, c := range clients {
			users = append(users, sbNaiveUser{Username: c.Name, Password: c.Password})
		}
		return sbNaiveInbound{
			Type:                  "naive",
			Tag:                   tag,
			Listen:                g.cfg.InboundListen,
			ListenPort:            ib.Port,
			Users:                 users,
			TLS:                   g.buildTLS(ib, []string{"h2", "http/1.1"}),
			Network:               ib.Settings.NaiveNetwork,
			QuicCongestionControl: ib.Settings.NaiveQuicCongestionCtrl,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported protocol %q", ib.Protocol)
	}
}

func buildTransport(ib *domain.Inbound) *sbTransport {
	switch ib.Transmission {
	case domain.TransmissionWS:
		return &sbTransport{Type: "ws", Path: ib.Settings.WSPath}
	case domain.TransmissionGRPC:
		return &sbTransport{Type: "grpc", ServiceName: ib.Settings.GRPCServiceName}
	case domain.TransmissionHTTPUpgrade:
		return &sbTransport{
			Type: "httpupgrade",
			Host: ib.Settings.HTTPUpgradeHost,
			Path: ib.Settings.HTTPUpgradePath,
		}
	default:
		return nil
	}
}

func (g *Generator) buildTLS(ib *domain.Inbound, defaultALPN []string) *sbInboundTLS {
	switch ib.TLS {
	case domain.TLSModeReality:
		host, port := parseDest(ib.Dest)
		return &sbInboundTLS{
			Enabled:    true,
			ServerName: ib.SNI,
			Reality: &sbReality{
				Enabled:    true,
				Handshake:  sbHandshake{Server: host, ServerPort: port},
				PrivateKey: ib.Settings.RealityPrivateKey,
				ShortID:    []string{ib.Settings.RealityShortID},
			},
		}
	case domain.TLSModeTLS:
		tls := &sbInboundTLS{
			Enabled:    true,
			ServerName: ib.SNI,
			ALPN:       defaultALPN,
		}
		switch {
		case ib.Settings.ACMEDomain != "":
			tls.ACME = &sbACME{Domain: []string{ib.Settings.ACMEDomain}, Email: ib.Settings.ACMEEmail}
		case ib.Settings.CertPath != "" && ib.Settings.KeyPath != "":
			tls.CertPath = ib.Settings.CertPath
			tls.KeyPath = ib.Settings.KeyPath
		default:
			// No ACME and no explicit cert: fall back to the panel-managed
			// self-signed keypair so tls inbounds (hysteria2/naive always need
			// tls) come up instead of emitting empty paths (SIN-52).
			tls.CertPath = g.cfg.DefaultTLSCertPath
			tls.KeyPath = g.cfg.DefaultTLSKeyPath
		}
		return tls
	default:
		return nil
	}
}

func parseDest(dest string) (string, int) {
	if dest == "" {
		return "", 443
	}
	host, portStr, err := net.SplitHostPort(dest)
	if err != nil {
		return dest, 443
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 443
	}
	return host, port
}

func inboundTag(ib *domain.Inbound) string {
	return fmt.Sprintf("%s-%d", ib.Protocol, ib.ID)
}
