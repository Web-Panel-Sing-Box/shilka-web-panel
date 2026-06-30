package singbox_test

import (
	"context"
	"encoding/json"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/repo"
	"sing-box-web-panel/internal/services/singbox"
)

type fakeSettingReader struct {
	data map[string]string
}

func (r fakeSettingReader) Get(_ context.Context, key string) (string, error) {
	v, ok := r.data[key]
	if !ok {
		return "", repo.ErrNotFound
	}
	return v, nil
}

type fakeInbounds struct{ list []domain.Inbound }

func (f fakeInbounds) ListEnabled(context.Context) ([]domain.Inbound, error) { return f.list, nil }

type fakeClients struct{ list []domain.Client }

func (f fakeClients) ListEnabled(context.Context) ([]domain.Client, error) { return f.list, nil }

func realityInbound() domain.Inbound {
	return domain.Inbound{
		ID: 1, Remark: "edge", Protocol: domain.ProtocolVLESS, Port: 44321,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443", Enabled: true,
		Settings: domain.InboundSettings{
			RealityPrivateKey: "PRIV", RealityPublicKey: "PUB", RealityShortID: "abcd1234",
			Flow: "xtls-rprx-vision",
		},
	}
}

func render(t *testing.T, source string) map[string]any {
	t.Helper()
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{list: []domain.Client{{ID: 1, InboundID: 1, Name: "alice", UUID: "uuid-1", Status: domain.ClientStatusActive}}},
		singbox.GeneratorConfig{
			ClashAPIAddress: "127.0.0.1:9090", ClashAPISecret: "secret",
			StatsSource: source, V2RayAPIListen: "127.0.0.1:8088",
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return cfg
}

func TestGeneratorClashSource(t *testing.T) {
	cfg := render(t, "clash")

	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("want 1 inbound, got %d", len(inbounds))
	}
	ib := inbounds[0].(map[string]any)
	if ib["type"] != "vless" {
		t.Errorf("want vless, got %v", ib["type"])
	}
	tls := ib["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)
	if reality["private_key"] != "PRIV" {
		t.Errorf("reality private_key missing")
	}
	users := ib["users"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["uuid"] != "uuid-1" {
		t.Errorf("unexpected users: %v", users)
	}

	exp := cfg["experimental"].(map[string]any)
	if _, ok := exp["clash_api"]; !ok {
		t.Errorf("clash_api must always be present")
	}
	if _, ok := exp["v2ray_api"]; ok {
		t.Errorf("v2ray_api must NOT be present for clash source")
	}
}

func TestGeneratorV2RaySource(t *testing.T) {
	cfg := render(t, "v2ray")
	exp := cfg["experimental"].(map[string]any)
	v2 := exp["v2ray_api"].(map[string]any)
	stats := v2["stats"].(map[string]any)
	users := stats["users"].([]any)
	if len(users) != 1 || users[0] != "alice" {
		t.Errorf("v2ray stats.users should list active client names, got %v", users)
	}
}

func TestGeneratorHTTPUpgradeTransport(t *testing.T) {
	ib := realityInbound()
	ib.TLS = domain.TLSModeNone
	ib.Transmission = domain.TransmissionHTTPUpgrade
	ib.Settings = domain.InboundSettings{
		HTTPUpgradePath: "/upgrade",
		HTTPUpgradeHost: "edge.example.com",
	}
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{ib}},
		fakeClients{list: []domain.Client{{ID: 1, InboundID: 1, Name: "alice", UUID: "uuid-1", Status: domain.ClientStatusActive}}},
		singbox.GeneratorConfig{ClashAPIAddress: "127.0.0.1:9090"},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	transport := cfg["inbounds"].([]any)[0].(map[string]any)["transport"].(map[string]any)
	if transport["type"] != "httpupgrade" || transport["path"] != "/upgrade" || transport["host"] != "edge.example.com" {
		t.Fatalf("transport = %#v", transport)
	}
}

func TestGeneratorEmitsPerClientOutboundAndRouteRule(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{list: []domain.Client{
			{ID: 1, InboundID: 1, Name: "alice", UUID: "uuid-1", Status: domain.ClientStatusActive},
			{ID: 42, InboundID: 1, Name: "тоха", UUID: "uuid-42", Status: domain.ClientStatusActive},
			{ID: 99, InboundID: 1, Name: "expired", Status: domain.ClientStatusExpired},
		}},
		singbox.GeneratorConfig{ClashAPIAddress: "127.0.0.1:9090"},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	outbounds := cfg["outbounds"].([]any)
	wantTags := map[string]bool{"direct": false, "user-1": false, "user-42": false}
	for _, ob := range outbounds {
		entry := ob.(map[string]any)
		tag, _ := entry["tag"].(string)
		if _, expected := wantTags[tag]; expected {
			wantTags[tag] = true
		}
		if expected := tag == "user-1" || tag == "user-42"; expected {
			if entry["type"] != "direct" {
				t.Errorf("per-client outbound %s should be direct, got %v", tag, entry["type"])
			}
		}
		if tag == "user-99" {
			t.Errorf("inactive client must not produce an outbound")
		}
	}
	for tag, found := range wantTags {
		if !found {
			t.Errorf("missing outbound tag %q in %v", tag, outbounds)
		}
	}

	route := cfg["route"].(map[string]any)
	rules := route["rules"].([]any)
	if len(rules) == 0 || rules[0].(map[string]any)["protocol"] != "bittorrent" {
		t.Fatalf("first route rule must be bittorrent reject, got %v", rules)
	}

	userRules := map[string]string{} // userName → outbound
	for _, r := range rules[1:] {
		entry := r.(map[string]any)
		users, _ := entry["auth_user"].([]any)
		ob, _ := entry["outbound"].(string)
		if len(users) == 1 {
			userRules[users[0].(string)] = ob
		}
	}
	if userRules["alice"] != "user-1" {
		t.Errorf("alice rule outbound = %q, want user-1; rules=%v", userRules["alice"], rules)
	}
	if userRules["тоха"] != "user-42" {
		t.Errorf("тоха rule outbound = %q, want user-42; rules=%v", userRules["тоха"], rules)
	}
	if _, ok := userRules["expired"]; ok {
		t.Errorf("inactive client must not produce a route rule")
	}

	if route["final"] != "direct" {
		t.Errorf("route.final = %v, want direct", route["final"])
	}
}

func TestGeneratorMultiInboundClientAppearsInAllInbounds(t *testing.T) {
	ib1 := realityInbound() // id 1
	ib2 := realityInbound()
	ib2.ID = 2
	ib2.Remark = "edge2"
	ib2.Port = 44322

	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{ib1, ib2}},
		fakeClients{list: []domain.Client{
			{ID: 7, InboundID: 1, InboundIDs: []int64{1, 2}, Name: "multi", UUID: "uuid-7", Status: domain.ClientStatusActive},
		}},
		singbox.GeneratorConfig{
			ClashAPIAddress: "127.0.0.1:9090",
			StatsSource:     "v2ray", V2RayAPIListen: "127.0.0.1:8088",
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The client must be emitted into BOTH inbound blocks.
	inbounds := cfg["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("want 2 inbounds, got %d", len(inbounds))
	}
	for _, raw := range inbounds {
		ib := raw.(map[string]any)
		users := ib["users"].([]any)
		if len(users) != 1 || users[0].(map[string]any)["uuid"] != "uuid-7" {
			t.Errorf("inbound %v should contain the multi-inbound client, got %v", ib["tag"], users)
		}
	}

	// Its per-client direct outbound must be emitted exactly once.
	outbounds := cfg["outbounds"].([]any)
	obCount := 0
	for _, raw := range outbounds {
		if raw.(map[string]any)["tag"] == "user-7" {
			obCount++
		}
	}
	if obCount != 1 {
		t.Errorf("want exactly 1 per-client outbound user-7, got %d", obCount)
	}

	// Its route rule must be emitted exactly once.
	rules := cfg["route"].(map[string]any)["rules"].([]any)
	ruleCount := 0
	for _, raw := range rules {
		users, _ := raw.(map[string]any)["auth_user"].([]any)
		if len(users) == 1 && users[0] == "multi" {
			ruleCount++
		}
	}
	if ruleCount != 1 {
		t.Errorf("want exactly 1 route rule for multi, got %d", ruleCount)
	}

	// v2ray stats must list the user once.
	stats := cfg["experimental"].(map[string]any)["v2ray_api"].(map[string]any)["stats"].(map[string]any)["users"].([]any)
	statCount := 0
	for _, u := range stats {
		if u == "multi" {
			statCount++
		}
	}
	if statCount != 1 {
		t.Errorf("want multi listed once in v2ray stats, got %d", statCount)
	}
}

func TestGeneratorSkipsInactiveClients(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{list: []domain.Client{{ID: 1, InboundID: 1, Name: "expired", Status: domain.ClientStatusExpired}}},
		singbox.GeneratorConfig{ClashAPIAddress: "127.0.0.1:9090"},
	)
	data, _ := gen.Render(context.Background())
	var cfg map[string]any
	json.Unmarshal(data, &cfg)
	ib := cfg["inbounds"].([]any)[0].(map[string]any)
	if users := ib["users"].([]any); len(users) != 0 {
		t.Errorf("inactive clients must be excluded, got %v", users)
	}
}

func TestGeneratorLogLevel_FromSettings(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{},
		singbox.GeneratorConfig{
			LogLevel: "info",
			Settings: fakeSettingReader{data: map[string]string{domain.SettingLogLevel: "debug"}},
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	log := cfg["log"].(map[string]any)
	if log["level"] != "debug" {
		t.Errorf("log.level = %v, want debug (from settings)", log["level"])
	}
}

func TestGeneratorLogLevel_FallbackWhenSettingsMissing(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{},
		singbox.GeneratorConfig{
			LogLevel: "warn",
			Settings: fakeSettingReader{data: map[string]string{}},
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	log := cfg["log"].(map[string]any)
	if log["level"] != "warn" {
		t.Errorf("log.level = %v, want warn (fallback from GeneratorConfig)", log["level"])
	}
}

func TestGeneratorLogLevel_FallbackWhenNoSettings(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{},
		singbox.GeneratorConfig{
			LogLevel: "error",
			Settings: nil,
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	log := cfg["log"].(map[string]any)
	if log["level"] != "error" {
		t.Errorf("log.level = %v, want error (from GeneratorConfig, settings nil)", log["level"])
	}
}

func TestGeneratorLogLevel_DefaultWhenBothEmpty(t *testing.T) {
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{realityInbound()}},
		fakeClients{},
		singbox.GeneratorConfig{
			LogLevel: "",
			Settings: fakeSettingReader{data: map[string]string{}},
		},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	json.Unmarshal(data, &cfg)

	log := cfg["log"].(map[string]any)
	if log["level"] != "info" {
		t.Errorf("log.level = %v, want info (hard default)", log["level"])
	}
}
