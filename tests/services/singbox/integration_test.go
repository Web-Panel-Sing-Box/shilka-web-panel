package singbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sing-box-web-panel/internal/domain"
	"sing-box-web-panel/internal/services/singbox"
)

func skipWithoutSingBox(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sing-box"); err != nil {
		t.Skip("sing-box not in PATH, skipping integration test")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Checker integration tests ---

func TestChecker_ValidConfig(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	cfg := singboxConfigJSON("", 10080, 19090)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, cfg, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	checker := singbox.NewChecker("sing-box", 5*time.Second)
	if err := checker.Check(context.Background(), path); err != nil {
		t.Fatalf("valid config should pass check: %v", err)
	}
}

func TestChecker_InvalidConfig(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	cfg := []byte(`{"type":"garbage"}`)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, cfg, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	checker := singbox.NewChecker("sing-box", 5*time.Second)
	if err := checker.Check(context.Background(), path); err == nil {
		t.Fatal("invalid config should fail check")
	}
}

func TestChecker_MissingFile(t *testing.T) {
	skipWithoutSingBox(t)

	checker := singbox.NewChecker("sing-box", 5*time.Second)
	if err := checker.Check(context.Background(), "/nonexistent/path.json"); err == nil {
		t.Fatal("missing file should fail check")
	}
}

func TestHTTPUpgradeGeneratedConfigPassesCheck(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{{
			ID: 1, Remark: "httpupgrade", Protocol: domain.ProtocolVLESS,
			Port: freeTCPPort(t), Transmission: domain.TransmissionHTTPUpgrade,
			TLS: domain.TLSModeNone, Enabled: true,
			Settings: domain.InboundSettings{
				HTTPUpgradePath: "/upgrade",
				HTTPUpgradeHost: "edge.example.com",
			},
		}}},
		fakeClients{list: []domain.Client{{
			ID: 1, InboundID: 1, Name: "alice",
			UUID:   "11111111-1111-4111-8111-111111111111",
			Status: domain.ClientStatusActive, Enabled: true,
		}}},
		singbox.GeneratorConfig{ClashAPIAddress: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))},
	)
	data, err := gen.Render(context.Background())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	checker := singbox.NewChecker("sing-box", 5*time.Second)
	if err := checker.Check(context.Background(), configPath); err != nil {
		t.Fatalf("HTTPUpgrade config should pass sing-box check: %v\n%s", err, data)
	}
}

// --- ProcessManager integration tests ---

func TestProcessManager_StartStopStatus(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	cfg := singboxConfigJSON(dir, 10081, 19091)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pm := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:       "subprocess",
		Binary:     "sing-box",
		ConfigPath: cfgPath,
		WorkingDir: dir,
	}, io.Discard, testLogger())

	ctx := context.Background()

	// Start
	if err := pm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Status should show running
	st, err := pm.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running {
		t.Fatal("core should be running after Start")
	}
	if st.PID == 0 {
		t.Error("PID should be non-zero")
	}
	if st.Version == "" {
		t.Error("version should not be empty")
	}
	t.Logf("core version: %s, pid: %d", st.Version, st.PID)

	// Stop
	if err := pm.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Status should show stopped
	st, err = pm.Status(ctx)
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if st.Running {
		t.Fatal("core should not be running after Stop")
	}
}

func TestProcessManager_Restart(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	cfg := singboxConfigJSON(dir, 10082, 19092)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, cfg, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	pm := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:       "subprocess",
		Binary:     "sing-box",
		ConfigPath: cfgPath,
		WorkingDir: dir,
	}, io.Discard, testLogger())

	ctx := context.Background()

	if err := pm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	stBefore, _ := pm.Status(ctx)

	if err := pm.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	stAfter, _ := pm.Status(ctx)
	if !stAfter.Running {
		t.Fatal("core should be running after Restart")
	}
	if stAfter.PID == stBefore.PID {
		t.Log("PID may be same after fast restart, this is normal")
	}

	pm.Stop(ctx)
}

// --- Applier integration tests ---

func TestApplier_ApplyIfMissing(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	pm := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:       "subprocess",
		Binary:     "sing-box",
		ConfigPath: filepath.Join(dir, "config.json"),
		WorkingDir: dir,
	}, io.Discard, testLogger())

	checker := singbox.NewChecker("sing-box", 5*time.Second)
	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{
			{ID: 1, Remark: "test", Protocol: domain.ProtocolVLESS, Port: 10083,
				Transmission: domain.TransmissionTCP, TLS: domain.TLSModeNone, Enabled: true,
			},
		}},
		fakeClients{},
		singbox.GeneratorConfig{ClashAPIAddress: fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))},
	)

	configPath := filepath.Join(dir, "config.json")
	applier := singbox.NewApplier(gen, checker, pm, nil, configPath, testLogger())

	ctx := context.Background()

	// ApplyIfMissing should generate config
	if err := applier.ApplyIfMissing(ctx); err != nil {
		t.Fatalf("ApplyIfMissing: %v", err)
	}

	// Config should exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config should have been generated")
	}

	// ApplyIfMissing again should be a no-op
	if err := applier.ApplyIfMissing(ctx); err != nil {
		t.Fatalf("second ApplyIfMissing: %v", err)
	}

	// Start core and verify config loaded
	if err := pm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pm.Stop(ctx)

	st, err := pm.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running {
		t.Fatal("core should be running with generated config")
	}
}

func TestApplier_Apply_InvalidConfig(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	configPath := filepath.Join(dir, "config.json")

	pm := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:       "subprocess",
		Binary:     "sing-box",
		ConfigPath: configPath,
		WorkingDir: dir,
	}, io.Discard, testLogger())

	checker := singbox.NewChecker("sing-box", 5*time.Second)

	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{
			{ID: 1, Remark: "bad", Protocol: "invalid-proto", Port: 54321,
				Transmission: domain.TransmissionTCP, TLS: domain.TLSModeNone, Enabled: true,
			},
		}},
		fakeClients{},
		singbox.GeneratorConfig{ClashAPIAddress: "127.0.0.1:19099"},
	)

	applier := singbox.NewApplier(gen, checker, pm, nil, configPath, testLogger())

	if err := applier.Apply(context.Background()); err == nil {
		t.Fatal("Apply should fail with unsupported protocol in generated config")
	}

	if _, err := os.Stat(configPath); err == nil {
		t.Error("config should NOT be installed on failed check")
	}
}

// --- Full pipeline integration test ---

func TestFullPipeline_ConfigToClashAPI(t *testing.T) {
	skipWithoutSingBox(t)
	dir := t.TempDir()

	const clashPort = 19090
	secret := "test-secret"

	checker := singbox.NewChecker("sing-box", 5*time.Second)

	gen := singbox.NewGenerator(
		fakeInbounds{list: []domain.Inbound{
			{ID: 1, Remark: "pipeline-test", Protocol: domain.ProtocolVLESS, Port: 10084,
				Transmission: domain.TransmissionTCP, TLS: domain.TLSModeNone, Enabled: true,
			},
		}},
		fakeClients{},
		singbox.GeneratorConfig{
			ClashAPIAddress: fmt.Sprintf("127.0.0.1:%d", clashPort),
			ClashAPISecret:  secret,
		},
	)

	configPath := filepath.Join(dir, "config.json")
	pm := singbox.NewProcessManager(singbox.ProcessConfig{
		Mode:       "subprocess",
		Binary:     "sing-box",
		ConfigPath: configPath,
		WorkingDir: dir,
	}, io.Discard, testLogger())

	applier := singbox.NewApplier(gen, checker, pm, nil, configPath, testLogger())

	ctx := context.Background()

	if err := applier.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := pm.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pm.Stop(ctx)

	clashURL := fmt.Sprintf("http://127.0.0.1:%d/connections", clashPort)
	resp, err := waitForClashAPI(ctx, clashURL, secret, 5*time.Second)
	if err != nil {
		t.Fatalf("Clash API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Clash API returned %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode clash response: %v", err)
	}
	t.Logf("Clash API response: downloadTotal=%v, uploadTotal=%v", data["downloadTotal"], data["uploadTotal"])

	st, err := pm.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running {
		t.Fatal("core should still be running")
	}
	t.Logf("Pipeline test: core running, pid=%d, version=%s", st.PID, st.Version)
}

func waitForClashAPI(ctx context.Context, url, secret string, timeout time.Duration) (*http.Response, error) {
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+secret)

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// singboxConfigJSON returns a minimal valid sing-box config JSON.
func singboxConfigJSON(workingDir string, port int, clashPort int) []byte {
	cachePath := ""
	if workingDir != "" {
		cachePath = filepath.Join(workingDir, "cache.db")
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []any{
			map[string]any{
				"type":        "mixed",
				"tag":         "mixed-in",
				"listen":      "127.0.0.1",
				"listen_port": port,
			},
		},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{"final": "direct"},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", clashPort),
				"secret":              "test-secret",
			},
		},
	}
	if cachePath != "" {
		cfg["experimental"].(map[string]any)["cache_file"] = map[string]any{
			"enabled": true,
			"path":    cachePath,
		}
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return data
}
