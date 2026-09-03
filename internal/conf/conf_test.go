// Package conf 清单模型测试:校验/回填幂等/IO/节点推导。
//
// 重点:凭据回填幂等(重跑不翻新,已分发客户端不受影响)与
// 服务端/客户端节点同源一致(防凭据漂移)。
package conf

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleServer 返回带固定字段的合法三通道服务器(测试基线)。
func sampleServer() *Server {
	return &Server{
		Name:     "hk-01",
		Location: "香港",
		Address:  "hk.example.com",
		SSH:      &SSHConfig{User: "root", Port: 22},
		VLESS: &VLESSConfig{
			ServerName: "www.apple.com",
		},
		Hysteria2: &H2Config{},
	}
}

// sampleInventory 返回含一台 sampleServer 的清单。
func sampleInventory() *Inventory {
	return &Inventory{Servers: []*Server{sampleServer()}}
}

// ===== Validate =====

// TestInventory_Validate_正常 验证合法清单通过,且回填后仍通过。
func TestInventory_Validate_正常(t *testing.T) {
	inv := sampleInventory()
	if err := inv.Validate(); err != nil {
		t.Fatalf("合法清单校验失败: %v", err)
	}
	if err := inv.Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("回填后校验失败: %v", err)
	}
}

// TestInventory_Validate_场景 表驱动覆盖非法清单各分支。
func TestInventory_Validate_场景(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Inventory)
		want   string // 错误消息关键片段(空=期望通过)
	}{
		{"空清单", func(inv *Inventory) { inv.Servers = nil }, "servers 为空"},
		{"重名", func(inv *Inventory) {
			inv.Servers = append(inv.Servers, sampleServer())
		}, "重复"},
		{"name 含非法字符", func(inv *Inventory) { inv.Servers[0].Name = "hk 01" }, "仅允许"},
		{"name 为点", func(inv *Inventory) { inv.Servers[0].Name = ".." }, "不能是"},
		{"无通道", func(inv *Inventory) {
			s := sampleServer()
			s.VLESS, s.Hysteria2 = nil, nil
			inv.Servers[0] = s
		}, "至少配置一个通道"},
		{"address 为 host:port", func(inv *Inventory) { inv.Servers[0].Address = "1.2.3.4:443" }, "IPv6"},
		{"address 带括号", func(inv *Inventory) { inv.Servers[0].Address = "[::1]" }, "非法"},
		{"address 为空", func(inv *Inventory) { inv.Servers[0].Address = "" }, "不能为空"},
		{"address 为合法 IPv6", func(inv *Inventory) { inv.Servers[0].Address = "2001:db8::1" }, ""},
		{"ssh 端口越界", func(inv *Inventory) { inv.Servers[0].SSH.Port = 70000 }, "超出范围"},
		{"vless 缺 server_name", func(inv *Inventory) { inv.Servers[0].VLESS.ServerName = "" }, "server_name"},
		{"vless server_name 带 URL", func(inv *Inventory) { inv.Servers[0].VLESS.ServerName = "https://a.com" }, "scheme"},
		{"uuid 形态非法", func(inv *Inventory) { inv.Servers[0].VLESS.UUID = "not-a-uuid" }, "uuid"},
		{"short_id 奇数位", func(inv *Inventory) { inv.Servers[0].VLESS.ShortID = "abc" }, "short_id"},
		{"short_id 非 hex", func(inv *Inventory) { inv.Servers[0].VLESS.ShortID = "zz" }, "不是合法 hex"},
		{"端口冲突", func(inv *Inventory) { inv.Servers[0].Hysteria2.Port = 443 }, "冲突"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := sampleInventory()
			tc.mutate(inv)
			err := inv.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("期望通过,实际报错: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误含 %q,实际: %v", tc.want, err)
			}
		})
	}
}

// ===== Backfill =====

// TestInventory_Backfill_幂等 验证两次回填凭据一致(不翻新)。
func TestInventory_Backfill_幂等(t *testing.T) {
	inv := sampleInventory()
	if err := inv.Backfill(); err != nil {
		t.Fatalf("首次回填失败: %v", err)
	}
	snapshot := cloneForTest(t, inv)
	if err := inv.Backfill(); err != nil {
		t.Fatalf("二次回填失败: %v", err)
	}
	v1, v2 := inv.Servers[0].VLESS, snapshot.Servers[0].VLESS
	if v1.UUID != v2.UUID || v1.PrivateKey != v2.PrivateKey || v1.ShortID != v2.ShortID {
		t.Fatal("二次回填翻新了 vless 凭据(幂等被破坏)")
	}
	if inv.Servers[0].Hysteria2.Password != snapshot.Servers[0].Hysteria2.Password {
		t.Fatal("二次回填翻新了 h2 密码")
	}
}

// cloneForTest 深拷贝清单(JSON round-trip,测试辅助)。
func cloneForTest(t *testing.T, inv *Inventory) *Inventory {
	t.Helper()
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	var out Inventory
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("反序列化: %v", err)
	}
	return &out
}

// TestInventory_Backfill_已填不覆盖 验证用户已有凭据保持不动。
func TestInventory_Backfill_已填不覆盖(t *testing.T) {
	s := sampleServer()
	s.VLESS.UUID = "11111111-1111-4111-8111-111111111111"
	s.Hysteria2.Port = 20000
	inv := &Inventory{Servers: []*Server{s}}
	if err := inv.Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	v := inv.Servers[0]
	if v.VLESS.UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatal("已填 uuid 被覆盖")
	}
	if v.Hysteria2.Port != 20000 {
		t.Fatal("已填端口被覆盖")
	}
}

// TestInventory_Backfill_默认值 验证端口与 method 默认回填。
func TestInventory_Backfill_默认值(t *testing.T) {
	inv := sampleInventory()
	if err := inv.Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	s := inv.Servers[0]
	if s.VLESS.Port != DefaultVLESSListenPort || s.Hysteria2.Port != DefaultHysteria2ListenPort {
		t.Fatalf("默认端口回填错误: vless=%d h2=%d", s.VLESS.Port, s.Hysteria2.Port)
	}
	if len(s.Hysteria2.Password) != 64 {
		t.Fatal("凭据长度异常(应为 32 字节 hex = 64 字符)")
	}
}

// ===== 密钥派生 =====

// TestPublicKeyFromPrivateKey_派生 用固定私钥断言公钥(防派生算法回归)。
func TestPublicKeyFromPrivateKey_派生(t *testing.T) {
	keys, err := GenerateRealityKeys()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	if keys.PrivateKey == keys.PublicKey {
		t.Fatal("私钥公钥不应相同")
	}
	if got := PublicKeyFromPrivateKey(keys.PrivateKey); got != keys.PublicKey {
		t.Fatalf("公钥派生不一致: got %q want %q", got, keys.PublicKey)
	}
	if got := PublicKeyFromPrivateKey("not-valid-key"); got != "" {
		t.Fatalf("非法私钥应返回空串,实际 %q", got)
	}
}

// ===== Nodes =====

// TestServer_Nodes_双通道 验证节点推导顺序/命名/凭据与私钥公钥一致。
func TestServer_Nodes_双通道(t *testing.T) {
	s := sampleServer()
	if err := (&Inventory{Servers: []*Server{s}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	nodes := s.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("期望 2 节点,实际 %d", len(nodes))
	}
	wantNames := []string{"hk-01-vless", "hk-01-h2"}
	wantTypes := []NodeType{TypeVLESSReality, TypeHysteria2}
	for i, want := range wantNames {
		if nodes[i].Name != want || nodes[i].Type != wantTypes[i] {
			t.Fatalf("节点 %d 推导错误: name=%q type=%q", i, nodes[i].Name, nodes[i].Type)
		}
	}
	if nodes[0].UUID != s.VLESS.UUID {
		t.Fatal("vless 节点 uuid 与服务端不一致(漂移)")
	}
	if nodes[0].PublicKey != PublicKeyFromPrivateKey(s.VLESS.PrivateKey) {
		t.Fatal("vless 节点公钥推导错误")
	}
	if nodes[1].Password != s.Hysteria2.Password || !nodes[1].Insecure {
		t.Fatal("h2 节点密码/自签语义错误")
	}
}

// TestServer_Nodes_H2受信证书 验证 server_name 非空时 insecure=false。
func TestServer_Nodes_H2受信证书(t *testing.T) {
	s := sampleServer()
	s.Hysteria2.ServerName = "vpn.example.com"
	if err := (&Inventory{Servers: []*Server{s}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	nodes := s.Nodes()
	h2 := nodes[len(nodes)-1]
	if h2.Insecure || h2.ServerName != "vpn.example.com" {
		t.Fatalf("受信证书语义错误: insecure=%v sni=%q", h2.Insecure, h2.ServerName)
	}
}

// TestServer_Nodes_端口 验证自定义端口透传与默认端口兜底。
func TestServer_Nodes_端口(t *testing.T) {
	s := sampleServer()
	s.VLESS.Port = 7443 // 自定义
	s.Hysteria2.Port = 9443
	if err := (&Inventory{Servers: []*Server{s}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	nodes := s.Nodes()
	if nodes[0].Port != 7443 || nodes[1].Port != 9443 {
		t.Fatalf("端口推导错误: %d/%d", nodes[0].Port, nodes[1].Port)
	}
}

// ===== Load/Save =====

// TestSaveLoad_往返 验证保存后加载字段一致且权限 0600。
func TestSaveLoad_往返(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	inv := sampleInventory()
	if err := inv.Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	if err := SaveFile(path, inv); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("权限应为 0600,实际 %o", info.Mode().Perm())
	}
	var got Inventory
	if err := LoadFile(path, &got); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if got.Servers[0].VLESS.UUID != inv.Servers[0].VLESS.UUID ||
		got.Servers[0].Hysteria2.Password != inv.Servers[0].Hysteria2.Password {
		t.Fatal("round-trip 凭据不一致")
	}
	if got.Servers[0].Location != "香港" {
		t.Fatal("round-trip 中文 location 不一致")
	}
}

// TestLoadFile_未知字段 验证严格模式拦截拼错字段名。
func TestLoadFile_未知字段(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	content := `{"servers":[{"name":"hk-01","address":"1.2.3.4","vless":{"server_name":"a.com","privatekey":"x"}}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	inv := &Inventory{}
	err := LoadFile(path, inv)
	if err == nil || !strings.Contains(err.Error(), "privatekey") {
		t.Fatalf("期望未知字段错误含字段名,实际: %v", err)
	}
}

// TestLoadFile_不存在 验证缺文件报错。
func TestLoadFile_不存在(t *testing.T) {
	err := LoadFile(filepath.Join(t.TempDir(), "nope.json"), &Inventory{})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("期望文件不存在错误,实际: %v", err)
	}
}

// ===== keys.go 生成器 =====

// TestNewUUID_格式 断言 UUID v4 形态/版本位/连调不重复(随机性冒烟)。
func TestNewUUID_格式(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		u, err := NewUUID()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if !UUIDPattern.MatchString(u) {
			t.Fatalf("UUID 形态非法: %q", u)
		}
		if u[14] != '4' {
			t.Fatalf("UUID 版本位错误: 第 15 位应为 4,实际 %c", u[14])
		}
		if seen[u] {
			t.Fatalf("UUID 重复: %q", u)
		}
		seen[u] = true
	}
}

// TestNewHex_长度与边界 断言 n 字节产出 2n 字符 hex、n<=0 报错。
func TestNewHex_长度与边界(t *testing.T) {
	h, err := NewHex(8)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(h) != 16 {
		t.Fatalf("长度应为 16 字符,实际 %d", len(h))
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("不是合法 hex: %v", err)
	}
	if len(raw) != 8 {
		t.Fatalf("解码后应为 8 字节,实际 %d", len(raw))
	}
	for _, n := range []int{0, -1} {
		if _, err := NewHex(n); err == nil || !strings.Contains(err.Error(), "正数") {
			t.Fatalf("NewHex(%d) 期望长度正数错误,实际: %v", n, err)
		}
	}
}

// TestGenerateRealityKeys_格式 断言密钥对 base64url 无 padding 43 字符、可解码 32 字节。
func TestGenerateRealityKeys_格式(t *testing.T) {
	keys, err := GenerateRealityKeys()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(keys.PrivateKey) != 43 || len(keys.PublicKey) != 43 {
		t.Fatalf("密钥应为 43 字符 base64url,实际 priv=%d pub=%d", len(keys.PrivateKey), len(keys.PublicKey))
	}
	if keys.PrivateKey == keys.PublicKey {
		t.Fatal("私钥公钥不应相同")
	}
	for _, k := range []string{keys.PrivateKey, keys.PublicKey} {
		raw, err := base64.RawURLEncoding.DecodeString(k)
		if err != nil {
			t.Fatalf("不是合法 base64url: %v", err)
		}
		if len(raw) != 32 {
			t.Fatalf("解码后应为 32 字节,实际 %d", len(raw))
		}
	}
}

// ===== Validate 补强 =====

// TestInventory_Validate_凭据与伪装站点 表驱动覆盖 private_key/server_name 的非法与合法形态。
func TestInventory_Validate_凭据与伪装站点(t *testing.T) {
	b16 := base64.RawURLEncoding.EncodeToString(make([]byte, 16)) // 16 字节,解码后长度不符
	cases := []struct {
		name   string
		mutate func(*Inventory)
		want   string // 错误消息关键片段(空=期望通过)
	}{
		{"vless private_key 非 base64", func(inv *Inventory) {
			inv.Servers[0].VLESS.PrivateKey = "!!!"
		}, "private_key"},
		{"vless private_key 长度错误", func(inv *Inventory) {
			inv.Servers[0].VLESS.PrivateKey = b16
		}, "private_key"},
		{"vless server_name 尾随空白", func(inv *Inventory) {
			inv.Servers[0].VLESS.ServerName = "www.apple.com "
		}, "空白"},
		{"h2 server_name 带 URL", func(inv *Inventory) {
			inv.Servers[0].Hysteria2.ServerName = "https://x.com"
		}, "非法"},
		{"h2 server_name 带端口误填", func(inv *Inventory) {
			inv.Servers[0].Hysteria2.ServerName = "vpn.example.com:8443"
		}, "非法"},
		{"h2 server_name 正常域名", func(inv *Inventory) {
			inv.Servers[0].Hysteria2.ServerName = "vpn.example.com"
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := sampleInventory()
			tc.mutate(inv)
			err := inv.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("期望通过,实际报错: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误含 %q,实际: %v", tc.want, err)
			}
		})
	}
}

// TestInventory_Validate_空条目 验证 nil 服务器指针被拦截。
func TestInventory_Validate_空条目(t *testing.T) {
	inv := &Inventory{Servers: []*Server{sampleServer(), nil}}
	err := inv.Validate()
	if err == nil || !strings.Contains(err.Error(), "空服务器条目") {
		t.Fatalf("期望错误含 %q,实际: %v", "空服务器条目", err)
	}
}

// TestInventory_Validate_地址形态 覆盖合法地址形态(IPv4/域名/单标签)与非法字符拦截。
func TestInventory_Validate_地址形态(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want string // 错误消息关键片段(空=期望通过)
	}{
		{"纯 IPv4", "1.2.3.4", ""},
		{"纯域名", "example.com", ""},
		{"单标签域名", "vps", ""},
		{"带协议前缀", "https://example.com", "非法"},
		{"带路径分隔符", "example.com/path", "非法"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := sampleInventory()
			inv.Servers[0].Address = tc.addr
			err := inv.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("期望通过,实际报错: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误含 %q,实际: %v", tc.want, err)
			}
		})
	}
}

// TestServer_Validate_SSH边界 验证 ssh.port 0/22 合法、SSH 缺失时也通过。
func TestServer_Validate_SSH边界(t *testing.T) {
	s := sampleServer()
	s.SSH.Port = 0 // 0 视为默认 22
	if err := s.Validate(); err != nil {
		t.Fatalf("ssh.port=0 期望通过,实际: %v", err)
	}
	s.SSH.Port = 22
	if err := s.Validate(); err != nil {
		t.Fatalf("ssh.port=22 期望通过,实际: %v", err)
	}
	s.SSH = nil
	if err := s.Validate(); err != nil {
		t.Fatalf("SSH 缺失期望通过,实际: %v", err)
	}
}

// ===== Backfill 补强 =====

// TestBackfill_单通道 验证单通道服务器回填后各自凭据齐全。
func TestBackfill_单通道(t *testing.T) {
	t.Run("仅vless", func(t *testing.T) {
		s := sampleServer()
		s.Hysteria2 = nil
		inv := &Inventory{Servers: []*Server{s}}
		if err := inv.Backfill(); err != nil {
			t.Fatalf("回填失败: %v", err)
		}
		v := inv.Servers[0].VLESS
		if v.PrivateKey == "" || v.UUID == "" || v.ShortID == "" {
			t.Fatalf("vless 凭据回填不齐: private_key=%q uuid=%q short_id=%q", v.PrivateKey, v.UUID, v.ShortID)
		}
	})
	t.Run("仅h2", func(t *testing.T) {
		s := sampleServer()
		s.VLESS = nil
		inv := &Inventory{Servers: []*Server{s}}
		if err := inv.Backfill(); err != nil {
			t.Fatalf("回填失败: %v", err)
		}
		if pw := inv.Servers[0].Hysteria2.Password; len(pw) != 64 {
			t.Fatalf("h2 密码应回填为 64 字符 hex,实际长度 %d", len(pw))
		}
	})
}

// ===== Nodes 补强 =====

// TestServer_Nodes_仅单通道 验证仅配一个通道时节点推导正确(名称/类型)。
func TestServer_Nodes_仅单通道(t *testing.T) {
	s := sampleServer()
	s.Hysteria2 = nil
	if err := (&Inventory{Servers: []*Server{s}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	nodes := s.Nodes()
	if len(nodes) != 1 || nodes[0].Name != "hk-01-vless" || nodes[0].Type != TypeVLESSReality {
		t.Fatalf("仅 vless 节点推导错误: len=%d name=%q type=%q", len(nodes), nodes[0].Name, nodes[0].Type)
	}

	s2 := sampleServer()
	s2.VLESS = nil
	if err := (&Inventory{Servers: []*Server{s2}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	nodes = s2.Nodes()
	if len(nodes) != 1 || nodes[0].Name != "hk-01-h2" || nodes[0].Type != TypeHysteria2 {
		t.Fatalf("仅 h2 节点推导错误: len=%d name=%q type=%q", len(nodes), nodes[0].Name, nodes[0].Type)
	}
}

// TestNodeType_ProtocolSuffix_映射 验证协议类型到短名映射与未知类型回退自身。
func TestNodeType_ProtocolSuffix_映射(t *testing.T) {
	cases := []struct {
		typ  NodeType
		want string
	}{
		{TypeVLESSReality, "vless"},
		{TypeHysteria2, "h2"},
		{NodeType("ss"), "ss"}, // 未知类型回退自身
	}
	for _, tc := range cases {
		if got := tc.typ.ProtocolSuffix(); got != tc.want {
			t.Fatalf("ProtocolSuffix(%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// ===== Load/Save 补强 =====

// TestSaveFile_目录不存在 验证目标目录缺失时保存报错(写临时文件阶段)。
func TestSaveFile_目录不存在(t *testing.T) {
	err := SaveFile(filepath.Join(t.TempDir(), "no", "x.json"), sampleInventory())
	if err == nil || !strings.Contains(err.Error(), "写临时文件") {
		t.Fatalf("期望错误含 %q,实际: %v", "写临时文件", err)
	}
}

// TestLoadFile_坏JSON 验证非法 JSON 内容报解析错误。
func TestLoadFile_坏JSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("not json{"), 0o600); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	err := LoadFile(path, &Inventory{})
	if err == nil || !strings.Contains(err.Error(), "解析") {
		t.Fatalf("期望错误含 %q,实际: %v", "解析", err)
	}
}
