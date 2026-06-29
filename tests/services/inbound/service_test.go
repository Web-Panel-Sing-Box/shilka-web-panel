package inbound_test

import (
	"context"
	"errors"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	svcinbound "sing-box-web-panel/internal/services/inbound"
)

type fakeRepo struct {
	items  map[int64]*domain.Inbound
	nextID int64
}

func newFakeRepo() *fakeRepo { return &fakeRepo{items: map[int64]*domain.Inbound{}} }

func (r *fakeRepo) Create(_ context.Context, ib *domain.Inbound) error {
	r.nextID++
	ib.ID = r.nextID
	cp := *ib
	r.items[ib.ID] = &cp
	return nil
}
func (r *fakeRepo) GetByID(_ context.Context, id int64) (*domain.Inbound, error) {
	if ib, ok := r.items[id]; ok {
		cp := *ib
		return &cp, nil
	}
	return nil, repo.ErrNotFound
}
func (r *fakeRepo) List(context.Context) ([]domain.Inbound, error) { return nil, nil }
func (r *fakeRepo) Update(_ context.Context, ib *domain.Inbound) error {
	r.items[ib.ID] = ib
	return nil
}
func (r *fakeRepo) SetEnabled(_ context.Context, id int64, e bool) error { return nil }
func (r *fakeRepo) Delete(_ context.Context, id int64) error             { return nil }

type fakeCounter struct{}

func (fakeCounter) CountByInbound(context.Context) (map[int64]int, error) {
	return map[int64]int{}, nil
}

func newService() *svcinbound.Service {
	return svcinbound.NewService(newFakeRepo(), fakeCounter{}, nil)
}

func TestCreateRealityGeneratesKeys(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "edge", Protocol: domain.ProtocolVLESS, Port: 44321,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ib.Settings.RealityPrivateKey == "" || ib.Settings.RealityPublicKey == "" {
		t.Error("reality keys should be generated")
	}
	if ib.Settings.RealityShortID == "" {
		t.Error("reality short id should be generated")
	}
	if ib.Settings.Flow != "xtls-rprx-vision" {
		t.Errorf("flow should be xtls-rprx-vision for tcp reality, got %q", ib.Settings.Flow)
	}
}

func TestCreateValidation(t *testing.T) {
	cases := []struct {
		name string
		in   svcinbound.Input
	}{
		{"reality without dest/sni", svcinbound.Input{Remark: "r", Protocol: domain.ProtocolVLESS, Port: 1, TLS: domain.TLSModeReality}},
		{"naive without tls", svcinbound.Input{Remark: "n", Protocol: domain.ProtocolNaive, Port: 1, TLS: domain.TLSModeNone}},
		{"reality on hysteria2", svcinbound.Input{Remark: "h", Protocol: domain.ProtocolHysteria2, Port: 1, TLS: domain.TLSModeReality}},
		{"bad port", svcinbound.Input{Remark: "p", Protocol: domain.ProtocolVLESS, Port: 0, TLS: domain.TLSModeNone}},
		{"empty remark", svcinbound.Input{Protocol: domain.ProtocolVLESS, Port: 1, TLS: domain.TLSModeNone}},
	}
	svc := newService()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), tc.in); !errors.Is(err, svcinbound.ErrValidation) {
				t.Errorf("want ErrValidation, got %v", err)
			}
		})
	}
}

func TestCreateNaiveNormalizesBothNetwork(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "naive", Protocol: domain.ProtocolNaive, Port: 4443,
		TLS: domain.TLSModeTLS, NaiveNetwork: "both",
	})
	if err != nil {
		t.Fatalf("create naive: %v", err)
	}
	if ib.Settings.NaiveNetwork != "" {
		t.Fatalf("naive both network should be stored as empty auto mode, got %q", ib.Settings.NaiveNetwork)
	}
}

func TestCreateHTTPUpgradePreservesSettingsAndGeneratesDefaultPath(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "upgrade", Protocol: domain.ProtocolVLESS, Port: 8080,
		Transmission: domain.TransmissionHTTPUpgrade, TLS: domain.TLSModeNone,
		HTTPUpgradeHost: "edge.example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ib.Transmission != domain.TransmissionHTTPUpgrade {
		t.Fatalf("transmission = %q, want httpupgrade", ib.Transmission)
	}
	if ib.Settings.HTTPUpgradeHost != "edge.example.com" {
		t.Fatalf("host = %q", ib.Settings.HTTPUpgradeHost)
	}
	if len(ib.Settings.HTTPUpgradePath) < 2 || ib.Settings.HTTPUpgradePath[0] != '/' {
		t.Fatalf("generated path = %q, want /<short-id>", ib.Settings.HTTPUpgradePath)
	}
}

func TestUpdateToHTTPUpgradeGeneratesDefaultPath(t *testing.T) {
	repo := newFakeRepo()
	svc := svcinbound.NewService(repo, fakeCounter{}, nil)
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "tcp", Protocol: domain.ProtocolVLESS, Port: 8080,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeNone,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Update(context.Background(), ib.ID, svcinbound.Input{
		Remark: "upgrade", Protocol: domain.ProtocolVLESS, Port: 8080,
		Transmission: domain.TransmissionHTTPUpgrade, TLS: domain.TLSModeNone,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Settings.HTTPUpgradePath) < 2 || updated.Settings.HTTPUpgradePath[0] != '/' {
		t.Fatalf("generated path = %q", updated.Settings.HTTPUpgradePath)
	}
}

func TestCreateTLSAllowInsecureAutoMode(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "hy2", Protocol: domain.ProtocolHysteria2, Port: 8443,
		TLS: domain.TLSModeTLS,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ib.Settings.AllowInsecure != nil {
		t.Fatalf("allow insecure override = %#v, want nil auto mode", *ib.Settings.AllowInsecure)
	}
	if !ib.EffectiveAllowInsecure() {
		t.Fatal("effective allow insecure should default to true without trusted cert material")
	}
}

func TestCreateTLSAllowInsecureExplicitFalse(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "hy2", Protocol: domain.ProtocolHysteria2, Port: 8443,
		TLS: domain.TLSModeTLS, AllowInsecure: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ib.Settings.AllowInsecure == nil || *ib.Settings.AllowInsecure {
		t.Fatalf("allow insecure override = %#v, want explicit false", ib.Settings.AllowInsecure)
	}
	if ib.EffectiveAllowInsecure() {
		t.Fatal("effective allow insecure should honor explicit false")
	}
}

func TestUpdateTLSAllowInsecureExplicitTrue(t *testing.T) {
	repo := newFakeRepo()
	svc := svcinbound.NewService(repo, fakeCounter{}, nil)
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "hy2", Protocol: domain.ProtocolHysteria2, Port: 8443,
		TLS: domain.TLSModeTLS, AllowInsecure: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	updated, err := svc.Update(context.Background(), ib.ID, svcinbound.Input{
		Remark: "hy2", Protocol: domain.ProtocolHysteria2, Port: 8443,
		TLS: domain.TLSModeTLS, AllowInsecure: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Settings.AllowInsecure == nil || !*updated.Settings.AllowInsecure {
		t.Fatalf("allow insecure override = %#v, want explicit true", updated.Settings.AllowInsecure)
	}
	if !updated.EffectiveAllowInsecure() {
		t.Fatal("effective allow insecure should honor explicit true")
	}
}

func TestToggleFlips(t *testing.T) {
	svc := newService()
	ib, err := svc.Create(context.Background(), svcinbound.Input{
		Remark: "x", Protocol: domain.ProtocolVLESS, Port: 5000, TLS: domain.TLSModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	toggled, err := svc.Toggle(context.Background(), ib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if toggled.Enabled == ib.Enabled {
		t.Errorf("toggle should flip enabled: before=%v after=%v", ib.Enabled, toggled.Enabled)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
