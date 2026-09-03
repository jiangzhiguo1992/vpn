// Package main CLI 冒烟测试:gen 端到端(临时清单 → 全产物)与幂等。
//
// 黄金断言:服务端 config.json 与客户端产物凭据一致(部署闭环防漂移),
// 二次 gen 产物与清单不变(幂等)。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSmokeInventory 写临时三通道清单(测试基线,不含凭据字段)。
func writeSmokeInventory(t *testing.T, path string) {
	t.Helper()
	content := `{
  "servers": [
    {
      "name": "hk-01",
      "location": "香港",
      "address": "hk.example.com",
      "ssh": {"user": "root", "port": 22},
      "vless": {"port": 443, "server_name": "www.apple.com"},
      "hysteria2": {"port": 8443}
    },
    {
      "name": "jp-01",
      "address": "2001:db8::1",
      "vless": {"server_name": "www.apple.com"}
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写清单: %v", err)
	}
}

// TestRunGen_端到端 gen 生成全产物,凭据回填清单,幂等,跨产物一致。
func TestRunGen_端到端(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	outDir := filepath.Join(dir, "dist")
	writeSmokeInventory(t, invPath)

	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("首次 gen 失败: %v", err)
	}
	// 1. 清单被回填凭据并保存
	rawInv, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("读清单: %v", err)
	}
	var inv struct {
		Servers []struct {
			Name  string `json:"name"`
			VLESS *struct {
				UUID string `json:"uuid"`
			} `json:"vless"`
			Hysteria2 *struct {
				Password string `json:"password"`
			} `json:"hysteria2"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rawInv, &inv); err != nil {
		t.Fatalf("解析清单: %v", err)
	}
	if inv.Servers[0].VLESS == nil || inv.Servers[0].VLESS.UUID == "" || inv.Servers[0].Hysteria2.Password == "" {
		t.Fatal("清单未被回填凭据")
	}
	if inv.Servers[1].VLESS == nil || inv.Servers[1].VLESS.UUID == "" {
		t.Fatal("第二台服务器未回填")
	}
	// 2. 产物齐全
	wantFiles := []string{
		"links.txt", "sub.txt", "clash.yaml", "sing-box.json",
		"servers/hk-01/config.json", "servers/hk-01/docker-compose.yml",
		"servers/hk-01/deploy.sh", "servers/hk-01/cert.sh",
		"servers/jp-01/config.json", "servers/jp-01/deploy.sh",
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Fatalf("缺产物 %s: %v", f, err)
		}
	}
	// jp-01 无 h2:不应有 cert.sh
	if _, err := os.Stat(filepath.Join(outDir, "servers/jp-01/cert.sh")); !os.IsNotExist(err) {
		t.Fatal("jp-01 不应有 cert.sh")
	}
	// 3. 黄金断言:服务端 config.json 的 uuid/密码 == links.txt(防漂移)
	cfg, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	links, _ := os.ReadFile(filepath.Join(outDir, "links.txt"))
	uuid := inv.Servers[0].VLESS.UUID
	h2Pass := inv.Servers[0].Hysteria2.Password
	if !strings.Contains(string(cfg), uuid) {
		t.Fatal("服务端 config.json 缺清单 uuid(漂移)")
	}
	if !strings.Contains(string(links), uuid) {
		t.Fatal("links.txt 缺清单 uuid(漂移)")
	}
	if !strings.Contains(string(cfg), h2Pass) || !strings.Contains(string(links), h2Pass) {
		t.Fatal("h2 密码在服务端/links 间漂移")
	}
	// 4. links.txt 每台服务器每通道一个节点:2 服务器 = vless+h2 + vless = 3 行
	lines := strings.Split(strings.TrimSpace(string(links)), "\n")
	if len(lines) != 3 {
		t.Fatalf("links 行数 = %d, want 3(2 服务器 2+1 通道)", len(lines))
	}
	// 5. 幂等:二次 gen 后产物与清单一致
	cfgBefore, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	linksBefore := string(links)
	rawInvBefore := string(rawInv)
	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("二次 gen 失败: %v", err)
	}
	cfgAfter, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	linksAfter, _ := os.ReadFile(filepath.Join(outDir, "links.txt"))
	rawInvAfter, _ := os.ReadFile(invPath)
	if string(cfgBefore) != string(cfgAfter) || linksBefore != string(linksAfter) || rawInvBefore != string(rawInvAfter) {
		t.Fatal("二次 gen 产物或清单变化(幂等被破坏)")
	}
}

// TestRunGen_客户端配置可解析 验证 sing-box.json/clash.yaml 关键结构。
func TestRunGen_客户端配置可解析(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	outDir := filepath.Join(dir, "dist")
	writeSmokeInventory(t, invPath)
	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("gen 失败: %v", err)
	}
	sb, _ := os.ReadFile(filepath.Join(outDir, "sing-box.json"))
	var m map[string]any
	if err := json.Unmarshal(sb, &m); err != nil {
		t.Fatalf("sing-box.json 不是合法 JSON: %v", err)
	}
	outbounds, _ := m["outbounds"].([]any)
	// direct + 3 节点 + auto + proxy = 6
	if len(outbounds) != 6 {
		t.Fatalf("outbounds 数量 = %d, want 6", len(outbounds))
	}
	clash, _ := os.ReadFile(filepath.Join(outDir, "clash.yaml"))
	out := string(clash)
	for _, want := range []string{"jp-01-vless", "hk-01-vless", "hk-01-h2", "MATCH,PROXY"} {
		if !strings.Contains(out, want) {
			t.Fatalf("clash.yaml 缺 %q", want)
		}
	}
}

// TestRunGen_清单缺失 缺清单时给出复制示例的引导错误。
func TestRunGen_清单缺失(t *testing.T) {
	err := runGen([]string{"-inventory", filepath.Join(t.TempDir(), "nope.json"), "-out", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "example-servers.json") {
		t.Fatalf("期望缺清单引导错误,实际: %v", err)
	}
}

// TestGoVersionAtLeast 版本判读:1.27+ 满足,旧版本拒绝,畸形串拒绝。
func TestGoVersionAtLeast(t *testing.T) {
	cases := []struct {
		ver  string
		want bool
	}{
		{"go version go1.27.0 darwin/arm64", true},
		{"go version go1.28.0 darwin/arm64", true},
		{"go version go2.0.0 linux/amd64", true},
		{"go version go1.26.7 darwin/arm64", false},
		{"go version go1.27 darwin/arm64", true}, // 无 patch 版本
		{"not-a-version", false},
	}
	for _, tc := range cases {
		if got := goVersionAtLeast(tc.ver, 1, 27); got != tc.want {
			t.Fatalf("goVersionAtLeast(%q) = %v, want %v", tc.ver, got, tc.want)
		}
	}
}
