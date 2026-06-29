// Package inbound provides CRUD and validation for sing-box inbounds, including
// server-side generation of Reality key material and transport defaults.
package inbound

import (
	"context"
	"errors"
	"fmt"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/lib/keys"
)

// Repo is the persistence contract for inbounds.
type Repo interface {
	Create(ctx context.Context, ib *domain.Inbound) error
	GetByID(ctx context.Context, id int64) (*domain.Inbound, error)
	List(ctx context.Context) ([]domain.Inbound, error)
	Update(ctx context.Context, ib *domain.Inbound) error
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	Delete(ctx context.Context, id int64) error
}

// ClientCounter reports how many clients are bound to each inbound.
type ClientCounter interface {
	CountByInbound(ctx context.Context) (map[int64]int, error)
}

// ConfigTrigger requests a (debounced) regenerate-and-apply of the live config.
// It may be nil during early wiring.
type ConfigTrigger interface {
	Trigger()
}

// Common service errors.
var (
	ErrValidation = errors.New("validation error")
	ErrNotFound   = errors.New("inbound not found")
	ErrPortInUse  = errors.New("port already in use")
)

// View couples an inbound with its current client count for list responses.
type View struct {
	Inbound     domain.Inbound
	ClientCount int
}

type Service struct {
	repo    Repo
	clients ClientCounter
	trigger ConfigTrigger
}

func NewService(repo Repo, clients ClientCounter, trigger ConfigTrigger) *Service {
	return &Service{repo: repo, clients: clients, trigger: trigger}
}

func (s *Service) notify() {
	if s.trigger != nil {
		s.trigger.Trigger()
	}
}

// Input carries the user-editable fields of an inbound.
type Input struct {
	Remark       string
	Protocol     domain.Protocol
	Port         int
	Transmission domain.Transmission
	TLS          domain.TLSMode
	SNI          string
	Dest         string
	// Optional TLS material for tls mode (mutually exclusive: ACME vs cert files).
	ACMEDomain string
	ACMEEmail  string
	CertPath   string
	KeyPath    string
	// Client-side subscription TLS verification override. nil means automatic.
	AllowInsecure *bool
	// VLESS multiplex.
	MultiplexEnabled bool
	// VLESS transport settings.
	WSPath          string
	GRPCServiceName string
	HTTPUpgradePath string
	HTTPUpgradeHost string
	// Hysteria2.
	Hy2UpMbps                int
	Hy2DownMbps              int
	Hy2IgnoreClientBandwidth bool
	Hy2ObfsPassword          string
	Hy2ObfsMinPacketSize     int
	Hy2ObfsMaxPacketSize     int
	Hy2Masquerade            string
	Hy2Network               string
	Hy2BrutalDebug           bool
	Hy2BbrProfile            string
	// Naive.
	NaiveNetwork            string
	NaiveQuicCongestionCtrl string
}

// applyTLSMaterial copies optional cert/ACME inputs into the inbound settings.
func applyTLSMaterial(ib *domain.Inbound, in Input) {
	ib.Settings.ACMEDomain = in.ACMEDomain
	ib.Settings.ACMEEmail = in.ACMEEmail
	ib.Settings.CertPath = in.CertPath
	ib.Settings.KeyPath = in.KeyPath
	ib.Settings.AllowInsecure = in.AllowInsecure
}

func applyProtocolSettings(ib *domain.Inbound, in Input) {
	ib.Settings.MultiplexEnabled = in.MultiplexEnabled
	ib.Settings.WSPath = in.WSPath
	ib.Settings.GRPCServiceName = in.GRPCServiceName
	ib.Settings.HTTPUpgradePath = in.HTTPUpgradePath
	ib.Settings.HTTPUpgradeHost = in.HTTPUpgradeHost
	ib.Settings.Hy2UpMbps = in.Hy2UpMbps
	ib.Settings.Hy2DownMbps = in.Hy2DownMbps
	ib.Settings.Hy2IgnoreClientBandwidth = in.Hy2IgnoreClientBandwidth
	ib.Settings.Hy2ObfsPassword = in.Hy2ObfsPassword
	ib.Settings.Hy2ObfsMinPacketSize = in.Hy2ObfsMinPacketSize
	ib.Settings.Hy2ObfsMaxPacketSize = in.Hy2ObfsMaxPacketSize
	ib.Settings.Hy2Masquerade = in.Hy2Masquerade
	ib.Settings.Hy2Network = in.Hy2Network
	ib.Settings.Hy2BrutalDebug = in.Hy2BrutalDebug
	ib.Settings.Hy2BbrProfile = in.Hy2BbrProfile
	ib.Settings.NaiveNetwork = normalizeNaiveNetwork(in.NaiveNetwork)
	ib.Settings.NaiveQuicCongestionCtrl = in.NaiveQuicCongestionCtrl
}

func normalizeNaiveNetwork(network string) string {
	if network == "both" {
		return ""
	}
	return network
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.clients.CountByInbound(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(list))
	for i := range list {
		views = append(views, View{Inbound: list[i], ClientCount: counts[list[i].ID]})
	}
	return views, nil
}

func (s *Service) ClientCount(ctx context.Context, inboundID int64) (int, error) {
	counts, err := s.clients.CountByInbound(ctx)
	if err != nil {
		return 0, err
	}
	return counts[inboundID], nil
}

func (s *Service) Get(ctx context.Context, id int64) (*domain.Inbound, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Create(ctx context.Context, in Input) (*domain.Inbound, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	ib := &domain.Inbound{
		Remark:       in.Remark,
		Protocol:     in.Protocol,
		Port:         in.Port,
		Transmission: normalizeTransmission(in.Protocol, in.Transmission),
		TLS:          in.TLS,
		SNI:          in.SNI,
		Dest:         in.Dest,
		Enabled:      true,
	}
	applyTLSMaterial(ib, in)
	applyProtocolSettings(ib, in)
	if err := s.applyGeneratedSettings(ib); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, ib); err != nil {
		return nil, err
	}
	s.notify()
	// Reload so the response carries DB-assigned timestamps.
	if created, err := s.repo.GetByID(ctx, ib.ID); err == nil {
		return created, nil
	}
	return ib, nil
}

func (s *Service) Update(ctx context.Context, id int64, in Input) (*domain.Inbound, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	ib, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if ib.NodeID != nil {
		return nil, fmt.Errorf("%w: remote inbound must be updated through its node", ErrValidation)
	}

	ib.Remark = in.Remark
	ib.Protocol = in.Protocol
	ib.Port = in.Port
	ib.Transmission = normalizeTransmission(in.Protocol, in.Transmission)
	ib.TLS = in.TLS
	ib.SNI = in.SNI
	ib.Dest = in.Dest
	applyTLSMaterial(ib, in)
	applyProtocolSettings(ib, in)

	// Fill missing Reality and transport defaults after protocol, security, or
	// transport changes. Existing generated values remain stable.
	if err := s.applyGeneratedSettings(ib); err != nil {
		return nil, err
	}

	if err := s.repo.Update(ctx, ib); err != nil {
		return nil, err
	}
	s.notify()
	return ib, nil
}

func (s *Service) Toggle(ctx context.Context, id int64) (*domain.Inbound, error) {
	ib, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if ib.NodeID != nil {
		return nil, fmt.Errorf("%w: remote inbound must be toggled through its node", ErrValidation)
	}
	ib.Enabled = !ib.Enabled
	if err := s.repo.SetEnabled(ctx, id, ib.Enabled); err != nil {
		return nil, err
	}
	s.notify()
	return ib, nil
}

func (s *Service) Clone(ctx context.Context, id int64) (*domain.Inbound, error) {
	src, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if src.NodeID != nil {
		return nil, fmt.Errorf("%w: remote inbound clone is not supported", ErrValidation)
	}
	clone := &domain.Inbound{
		Remark:       src.Remark + "-copy",
		Protocol:     src.Protocol,
		Port:         randomPort(),
		Transmission: src.Transmission,
		TLS:          src.TLS,
		SNI:          src.SNI,
		Dest:         src.Dest,
		Enabled:      false,
		Settings: domain.InboundSettings{
			AllowInsecure: cloneBoolPtr(src.Settings.AllowInsecure),
		},
	}
	if err := s.applyGeneratedSettings(clone); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, clone); err != nil {
		return nil, err
	}
	s.notify()
	return clone, nil
}

func cloneBoolPtr(v *bool) *bool {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	ib, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if ib.NodeID != nil {
		return fmt.Errorf("%w: remote inbound must be deleted through its node", ErrValidation)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.notify()
	return nil
}

// applyGeneratedSettings fills Reality keys, short ID, flow and transport
// defaults that are generated server-side.
func (s *Service) applyGeneratedSettings(ib *domain.Inbound) error {
	if ib.TLS == domain.TLSModeReality {
		if ib.Settings.RealityPrivateKey == "" {
			kp, err := keys.GenerateRealityKeyPair()
			if err != nil {
				return err
			}
			ib.Settings.RealityPrivateKey = kp.PrivateKey
			ib.Settings.RealityPublicKey = kp.PublicKey
		}
		if ib.Settings.RealityShortID == "" {
			sid, err := keys.GenerateShortID()
			if err != nil {
				return err
			}
			ib.Settings.RealityShortID = sid
		}
	}
	ib.Settings.Flow = realityFlow(ib)

	switch ib.Transmission {
	case domain.TransmissionWS:
		if ib.Settings.WSPath == "" {
			suffix, err := keys.GenerateShortID()
			if err != nil {
				return err
			}
			ib.Settings.WSPath = "/" + suffix
		}
	case domain.TransmissionGRPC:
		if ib.Settings.GRPCServiceName == "" {
			name, err := keys.GenerateShortID()
			if err != nil {
				return err
			}
			ib.Settings.GRPCServiceName = name
		}
	case domain.TransmissionHTTPUpgrade:
		if ib.Settings.HTTPUpgradePath == "" {
			suffix, err := keys.GenerateShortID()
			if err != nil {
				return err
			}
			ib.Settings.HTTPUpgradePath = "/" + suffix
		}
	}
	return nil
}

// realityFlow returns the VLESS flow value. xtls-rprx-vision is only valid for
// raw TCP with Reality; other transports must leave flow empty.
func realityFlow(ib *domain.Inbound) string {
	if ib.Protocol == domain.ProtocolVLESS &&
		ib.TLS == domain.TLSModeReality &&
		ib.Transmission == domain.TransmissionTCP {
		return "xtls-rprx-vision"
	}
	return ""
}

func normalizeTransmission(p domain.Protocol, t domain.Transmission) domain.Transmission {
	if p != domain.ProtocolVLESS {
		return domain.TransmissionTCP
	}
	switch t {
	case domain.TransmissionTCP, domain.TransmissionWS, domain.TransmissionGRPC, domain.TransmissionHTTPUpgrade:
		return t
	default:
		return domain.TransmissionTCP
	}
}

func validate(in Input) error {
	if in.Remark == "" {
		return fmt.Errorf("%w: remark is required", ErrValidation)
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", ErrValidation)
	}
	switch in.Protocol {
	case domain.ProtocolVLESS, domain.ProtocolNaive, domain.ProtocolHysteria2:
	default:
		return fmt.Errorf("%w: unsupported protocol %q", ErrValidation, in.Protocol)
	}
	if in.TLS == domain.TLSModeReality {
		if in.Protocol != domain.ProtocolVLESS {
			return fmt.Errorf("%w: reality is only supported for vless", ErrValidation)
		}
		if in.Dest == "" || in.SNI == "" {
			return fmt.Errorf("%w: reality requires dest and sni", ErrValidation)
		}
	}
	if in.Protocol == domain.ProtocolNaive && in.TLS == domain.TLSModeNone {
		return fmt.Errorf("%w: naive requires tls", ErrValidation)
	}
	if in.Protocol == domain.ProtocolHysteria2 && in.TLS == domain.TLSModeReality {
		return fmt.Errorf("%w: hysteria2 does not support reality", ErrValidation)
	}
	// Custom cert material must be complete: a lone cert or key path produces a
	// broken tls block. (No source at all is fine: the generator falls back to a
	// panel-managed self-signed cert — SIN-52.)
	if (in.CertPath == "") != (in.KeyPath == "") {
		return fmt.Errorf("%w: cert_path and key_path must both be set", ErrValidation)
	}
	// Hysteria2: bandwidth limits.
	if in.Protocol == domain.ProtocolHysteria2 {
		if in.Hy2UpMbps < 0 {
			return fmt.Errorf("%w: up_mbps must be >= 0", ErrValidation)
		}
		if in.Hy2DownMbps < 0 {
			return fmt.Errorf("%w: down_mbps must be >= 0", ErrValidation)
		}
		if in.Hy2ObfsPassword != "" && in.TLS == domain.TLSModeNone {
			return fmt.Errorf("%w: obfs requires tls", ErrValidation)
		}
		switch in.Hy2Network {
		case "", "tcp", "udp":
		default:
			return fmt.Errorf("%w: hysteria2 network must be tcp, udp, or empty", ErrValidation)
		}
		switch in.Hy2BbrProfile {
		case "", "conservative", "standard", "aggressive":
		default:
			return fmt.Errorf("%w: bbr_profile must be conservative, standard, or aggressive", ErrValidation)
		}
	}
	// Naive: network must be one of the allowed values.
	if in.Protocol == domain.ProtocolNaive {
		switch in.NaiveNetwork {
		case "", "tcp", "udp", "both":
		default:
			return fmt.Errorf("%w: naive network must be tcp, udp, or empty", ErrValidation)
		}
	}
	return nil
}
