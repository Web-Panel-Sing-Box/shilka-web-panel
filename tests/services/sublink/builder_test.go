package sublink_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/sublink"
)

func TestBuildLinkVLESSReality(t *testing.T) {
	ib := &domain.Inbound{
		ID: 1, Remark: "edge", Protocol: domain.ProtocolVLESS, Port: 44321,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443",
		Settings: domain.InboundSettings{
			RealityPublicKey: "PUBKEY", RealityShortID: "abcd1234", Flow: "xtls-rprx-vision",
		},
	}
	c := &domain.Client{Name: "alice", UUID: "11111111-1111-4111-8111-111111111111"}

	link := sublink.BuildLink(ib, c, "203.0.113.10")
	for _, want := range []string{
		"vless://11111111-1111-4111-8111-111111111111@203.0.113.10:44321",
		"security=reality", "pbk=PUBKEY", "sid=abcd1234", "flow=xtls-rprx-vision",
		"sni=www.cloudflare.com", "#alice",
	} {
		if !strings.Contains(link, want) {
			t.Errorf("vless reality link missing %q\n got: %s", want, link)
		}
	}
}

func TestBuildLinkHysteria2(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	link := sublink.BuildLink(ib, c, "panel.example")
	for _, want := range []string{"hysteria2://", "secret-pass@panel.example:51005", "sni=panel.example", "insecure=1", "#bob"} {
		if !strings.Contains(link, want) {
			t.Errorf("hysteria2 link missing %q\n got: %s", want, link)
		}
	}
}

func TestBuildLinkHysteria2AllowInsecureFalse(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{
			AllowInsecure: boolPtr(false),
		},
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	link := sublink.BuildLink(ib, c, "panel.example")
	if got := queryValue(t, link, "insecure"); got != "0" {
		t.Fatalf("insecure = %q, want 0\nlink: %s", got, link)
	}
}

func TestBuildLinkHysteria2WithObfs(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{
			Hy2ObfsPassword: "obfs-secret",
		},
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	link := sublink.BuildLink(ib, c, "panel.example")
	if got := queryValue(t, link, "obfs"); got != "salamander" {
		t.Fatalf("obfs = %q, want salamander\nlink: %s", got, link)
	}
	if got := queryValue(t, link, "obfs-password"); got != "obfs-secret" {
		t.Fatalf("obfs-password = %q, want obfs-secret\nlink: %s", got, link)
	}
}

func TestBuildLinkVLESSTLSAllowInsecure(t *testing.T) {
	ib := &domain.Inbound{
		ID: 4, Protocol: domain.ProtocolVLESS, Port: 443,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeTLS,
		SNI: "panel.example",
	}
	c := &domain.Client{Name: "dave", UUID: "11111111-1111-4111-8111-111111111111"}

	link := sublink.BuildLink(ib, c, "panel.example")
	if got := queryValue(t, link, "allowInsecure"); got != "1" {
		t.Fatalf("allowInsecure = %q, want 1\nlink: %s", got, link)
	}
}

func TestBuildLinkVLESSRealityOmitsAllowInsecure(t *testing.T) {
	ib := &domain.Inbound{
		ID: 5, Protocol: domain.ProtocolVLESS, Port: 443,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443",
		Settings: domain.InboundSettings{
			AllowInsecure:     boolPtr(true),
			RealityPublicKey:  "PUBKEY",
			RealityPrivateKey: "PRIVATE",
			RealityShortID:    "abcd1234",
		},
	}
	c := &domain.Client{Name: "erin", UUID: "11111111-1111-4111-8111-111111111111"}

	link := sublink.BuildLink(ib, c, "panel.example")
	if got := queryValue(t, link, "allowInsecure"); got != "" {
		t.Fatalf("allowInsecure = %q, want empty\nlink: %s", got, link)
	}
}

func TestBuildLinkNaive(t *testing.T) {
	ib := &domain.Inbound{ID: 3, Protocol: domain.ProtocolNaive, Port: 38119, TLS: domain.TLSModeTLS}
	c := &domain.Client{Name: "carol", Password: "pw"}

	link := sublink.BuildLink(ib, c, "panel.example")
	if !strings.HasPrefix(link, "naive+https://carol:pw@panel.example:38119") {
		t.Errorf("unexpected naive link: %s", link)
	}
	if got := queryValue(t, link, "allowInsecure"); got != "1" {
		t.Fatalf("allowInsecure = %q, want 1\nlink: %s", got, link)
	}
}

func TestRenderBase64(t *testing.T) {
	ib := &domain.Inbound{ID: 1, Protocol: domain.ProtocolVLESS, Port: 443, TLS: domain.TLSModeNone}
	c := &domain.Client{Name: "x", UUID: "u"}

	res, err := sublink.Render(sublink.FormatBase64, ib, c, "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Body) == 0 || strings.Contains(string(res.Body), "vless://") {
		t.Errorf("base64 output should be encoded, got: %s", res.Body)
	}
}

func TestBuildClientConfigAllowInsecure(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	body, err := sublink.BuildClientConfig(ib, c, "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	tls := firstOutboundTLS(t, body)
	if got := tls["insecure"]; got != true {
		t.Fatalf("tls.insecure = %#v, want true\nconfig: %s", got, body)
	}
}

func TestBuildClientConfigHysteria2MirrorsProtocolOptions(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{
			Hy2UpMbps:            120,
			Hy2DownMbps:          240,
			Hy2ObfsPassword:      "obfs-secret",
			Hy2ObfsMinPacketSize: 600,
			Hy2ObfsMaxPacketSize: 1100,
			Hy2Network:           "udp",
			Hy2BbrProfile:        "aggressive",
			Hy2BrutalDebug:       true,
		},
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	body, err := sublink.BuildClientConfig(ib, c, "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	out := firstOutbound(t, body)
	if out["up_mbps"] != float64(120) || out["down_mbps"] != float64(240) {
		t.Fatalf("bandwidth = %#v/%#v, want 120/240\nconfig: %s", out["up_mbps"], out["down_mbps"], body)
	}
	if out["network"] != "udp" {
		t.Fatalf("network = %#v, want udp\nconfig: %s", out["network"], body)
	}
	if out["bbr_profile"] != "aggressive" {
		t.Fatalf("bbr_profile = %#v, want aggressive\nconfig: %s", out["bbr_profile"], body)
	}
	if out["brutal_debug"] != true {
		t.Fatalf("brutal_debug = %#v, want true\nconfig: %s", out["brutal_debug"], body)
	}
	obfs, ok := out["obfs"].(map[string]any)
	if !ok {
		t.Fatalf("obfs missing or invalid: %#v\nconfig: %s", out["obfs"], body)
	}
	if obfs["type"] != "salamander" || obfs["password"] != "obfs-secret" {
		t.Fatalf("obfs = %#v, want salamander/obfs-secret\nconfig: %s", obfs, body)
	}
	if obfs["min_packet_size"] != float64(600) || obfs["max_packet_size"] != float64(1100) {
		t.Fatalf("obfs packet sizes = %#v/%#v, want 600/1100\nconfig: %s", obfs["min_packet_size"], obfs["max_packet_size"], body)
	}
	tls := firstOutboundTLS(t, body)
	if tls["server_name"] != "panel.example" || tls["insecure"] != true {
		t.Fatalf("tls = %#v, want server_name panel.example and insecure true\nconfig: %s", tls, body)
	}
}

func TestBuildClientConfigTrustedTLSOmitsInsecure(t *testing.T) {
	ib := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{
			ACMEDomain: "panel.example",
		},
	}
	c := &domain.Client{Name: "bob", Password: "secret-pass"}

	body, err := sublink.BuildClientConfig(ib, c, "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	tls := firstOutboundTLS(t, body)
	if _, ok := tls["insecure"]; ok {
		t.Fatalf("tls.insecure should be omitted for trusted TLS\nconfig: %s", body)
	}
}

func TestBuildClientConfigNaiveSelfSignedReturnsClearError(t *testing.T) {
	ib := &domain.Inbound{
		ID: 3, Protocol: domain.ProtocolNaive, Port: 38119,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
	}
	c := &domain.Client{Name: "carol", Password: "pw"}

	_, err := sublink.BuildClientConfig(ib, c, "panel.example")
	if !errors.Is(err, sublink.ErrNaiveJSONRequiresTrustedTLS) {
		t.Fatalf("error = %v, want ErrNaiveJSONRequiresTrustedTLS", err)
	}
}

func TestBuildClientConfigNaiveTrustedTLSOmitsInsecure(t *testing.T) {
	ib := &domain.Inbound{
		ID: 3, Protocol: domain.ProtocolNaive, Port: 38119,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{
			ACMEDomain: "panel.example",
		},
	}
	c := &domain.Client{Name: "carol", Password: "pw"}

	body, err := sublink.BuildClientConfig(ib, c, "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	tls := firstOutboundTLS(t, body)
	if _, ok := tls["insecure"]; ok {
		t.Fatalf("naive tls.insecure should be omitted\nconfig: %s", body)
	}
	if tls["server_name"] != "panel.example" {
		t.Fatalf("server_name = %#v, want panel.example\nconfig: %s", tls["server_name"], body)
	}
}

func TestRenderMultiPlainJoinsAllLinks(t *testing.T) {
	ib1 := &domain.Inbound{
		ID: 1, Remark: "edge", Protocol: domain.ProtocolVLESS, Port: 44321,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443",
		Settings: domain.InboundSettings{RealityPublicKey: "PUB", RealityShortID: "abcd1234"},
	}
	ib2 := &domain.Inbound{
		ID: 2, Remark: "hy2", Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
	}
	c := &domain.Client{Name: "multi", UUID: "11111111-1111-4111-8111-111111111111", Password: "pw"}

	res, err := sublink.RenderMulti(sublink.FormatPlain, []*domain.Inbound{ib1, ib2}, c, "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	body := string(res.Body)
	if !strings.Contains(body, "vless://") || !strings.Contains(body, "hysteria2://") {
		t.Fatalf("plain multi should contain both protocol links, got: %s", body)
	}
	if lines := strings.Split(strings.TrimSpace(body), "\n"); len(lines) != 2 {
		t.Fatalf("want 2 links, got %d: %s", len(lines), body)
	}
}

func TestRenderMultiJSONBuildsSelector(t *testing.T) {
	ib1 := &domain.Inbound{
		ID: 1, Protocol: domain.ProtocolVLESS, Port: 443,
		Transmission: domain.TransmissionTCP, TLS: domain.TLSModeReality,
		SNI: "www.cloudflare.com", Dest: "www.cloudflare.com:443",
		Settings: domain.InboundSettings{RealityPublicKey: "PUB", RealityShortID: "abcd1234"},
	}
	ib2 := &domain.Inbound{
		ID: 2, Protocol: domain.ProtocolHysteria2, Port: 51005,
		TLS: domain.TLSModeTLS, SNI: "panel.example",
		Settings: domain.InboundSettings{ACMEDomain: "panel.example"},
	}
	c := &domain.Client{Name: "multi", UUID: "u", Password: "pw"}

	res, err := sublink.RenderMulti(sublink.FormatJSON, []*domain.Inbound{ib1, ib2}, c, "panel.example")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     map[string]any   `json:"route"`
	}
	if err := json.Unmarshal(res.Body, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var selector map[string]any
	tags := map[string]bool{}
	for _, ob := range cfg.Outbounds {
		tags[ob["tag"].(string)] = true
		if ob["tag"] == "proxy" && ob["type"] == "selector" {
			selector = ob
		}
	}
	if selector == nil {
		t.Fatalf("missing proxy selector: %s", res.Body)
	}
	if sel := selector["outbounds"].([]any); len(sel) != 2 {
		t.Fatalf("selector should reference 2 outbounds, got %v", sel)
	}
	if !tags["proxy-1"] || !tags["proxy-2"] || !tags["direct"] {
		t.Fatalf("missing expected outbound tags: %v", tags)
	}
	if cfg.Route["final"] != "proxy" {
		t.Fatalf("route.final = %v, want proxy", cfg.Route["final"])
	}
}

func queryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	return u.Query().Get(key)
}

func firstOutbound(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.Outbounds) == 0 {
		t.Fatal("expected at least one outbound")
	}
	return cfg.Outbounds[0]
}

func firstOutboundTLS(t *testing.T, body []byte) map[string]any {
	t.Helper()
	out := firstOutbound(t, body)
	tls, ok := out["tls"].(map[string]any)
	if !ok {
		t.Fatalf("first outbound tls is missing or invalid: %#v", out["tls"])
	}
	return tls
}

func boolPtr(v bool) *bool {
	return &v
}
