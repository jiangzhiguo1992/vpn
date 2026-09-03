// Package deploy 部署执行测试:命令构造与执行序列。
//
// 重点:ssh/scp 参数形态(端口标志区分、IPv6 括号、非交互三件套)
// 与 Run 的逐台调用序列(注入 mock,不实际连接)。
package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpn/internal/conf"
)

// fixtureServer 返回含 SSH 配置的合法服务器(测试基线)。
func fixtureServer() *conf.Server {
	return &conf.Server{
		Name:    "hk-01",
		Address: "hk.example.com",
		SSH:     &conf.SSHConfig{User: "root", Port: 22},
		VLESS:   &conf.VLESSConfig{ServerName: "www.apple.com"},
	}
}

// ===== sshCommand / scpCommand =====

// TestSSHCommand_参数 验证默认 root@22、非 22 端口、IPv6 括号、超时选项。
func TestSSHCommand_参数(t *testing.T) {
	cases := []struct {
		name string
		srv  *conf.Server
		want []string // 关键参数片段(顺序敏感部分)
	}{
		{"默认 user/端口", fixtureServer(), []string{"root@hk.example.com", "echo hi"}},
		{"自定义端口", func() *conf.Server {
			s := fixtureServer()
			s.SSH.Port = 2222
			return s
		}(), []string{"-p", "2222", "root@hk.example.com"}},
		{"自定义用户", func() *conf.Server {
			s := fixtureServer()
			s.SSH.User = "ubuntu"
			return s
		}(), []string{"ubuntu@hk.example.com"}},
		{"无 ssh 块默认 root", func() *conf.Server {
			s := fixtureServer()
			s.SSH = nil
			return s
		}(), []string{"root@hk.example.com"}},
		{"IPv6 加括号", func() *conf.Server {
			s := fixtureServer()
			s.Address = "2001:db8::1"
			return s
		}(), []string{"root@[2001:db8::1]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := sshCommand(tc.srv, "echo hi")
			joined := strings.Join(args, " ")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Fatalf("ssh 参数缺 %q: %v", w, args)
				}
			}
		})
	}
	// 非交互三件套恒在
	args := sshCommand(fixtureServer(), "x")
	joined := strings.Join(args, " ")
	for _, w := range []string{"ConnectTimeout=10", "StrictHostKeyChecking=accept-new", "BatchMode=yes"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("ssh 参数缺 %q: %v", w, args)
		}
	}
}

// TestSCPCommand_参数 验证端口标志为大写 -P 且目标为远程目录。
func TestSCPCommand_参数(t *testing.T) {
	s := fixtureServer()
	s.SSH.Port = 2222
	args := scpCommand(s, "config.json", "deploy.sh")
	joined := strings.Join(args, " ")
	for _, w := range []string{"-P", "2222", "config.json", "deploy.sh", "root@hk.example.com:" + RemoteDir + "/"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("scp 参数缺 %q: %v", w, args)
		}
	}
}

// ===== Run(注入 mock) =====

// TestRun_成功序列 验证每台服务器 3 次远程调用(mkdir → scp → deploy.sh)。
func TestRun_成功序列(t *testing.T) {
	s := fixtureServer()
	outDir := t.TempDir()
	prodDir := filepath.Join(outDir, "servers", "hk-01")
	if err := os.MkdirAll(prodDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prodDir, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("写产物: %v", err)
	}
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	defer func() { runCmd = oldRunCmd }()

	inv := &conf.Inventory{Servers: []*conf.Server{s}}
	if err := Run(inv, outDir); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("调用数 = %d, want 3\n%v", len(calls), calls)
	}
	if !strings.Contains(calls[0], "ssh") || !strings.Contains(calls[0], RemoteDir) || !strings.Contains(calls[0], "rm -f") {
		t.Fatalf("第一步应为建目录+清 cert.sh: %s", calls[0])
	}
	if !strings.Contains(calls[1], "scp") || !strings.Contains(calls[1], "deploy.sh") {
		t.Fatalf("第二步应为上传产物: %s", calls[1])
	}
	if !strings.Contains(calls[2], "ssh") || !strings.Contains(calls[2], "sh deploy.sh") {
		t.Fatalf("第三步应为远程执行 deploy.sh: %s", calls[2])
	}
}

// TestRun_产物缺失 验证未 gen 时给出引导错误且不发起任何调用。
func TestRun_产物缺失(t *testing.T) {
	inv := &conf.Inventory{Servers: []*conf.Server{fixtureServer()}}
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name)
		return nil
	}
	defer func() { runCmd = oldRunCmd }()
	err := Run(inv, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "先 make gen") {
		t.Fatalf("期望产物缺失引导错误,实际: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("产物缺失不应发起远程调用: %v", calls)
	}
}

// TestRun_失败即停 验证首台失败立即返回(含服务器名),不继续第二台。
func TestRun_失败即停(t *testing.T) {
	s1 := fixtureServer()
	s2 := fixtureServer()
	s2.Name = "jp-01"
	outDir := t.TempDir()
	for _, n := range []string{"hk-01", "jp-01"} {
		d := filepath.Join(outDir, "servers", n)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		os.WriteFile(filepath.Join(d, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755)
	}
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		if name == "ssh" && strings.Contains(strings.Join(args, " "), "sh deploy.sh") {
			return errMock
		}
		return nil
	}
	defer func() { runCmd = oldRunCmd }()
	inv := &conf.Inventory{Servers: []*conf.Server{s1, s2}}
	err := Run(inv, outDir)
	if err == nil || !strings.Contains(err.Error(), "hk-01") {
		t.Fatalf("期望失败错误含服务器名,实际: %v", err)
	}
}

// errMock 是 mock 失败哨兵。
var errMock = &mockError{}

type mockError struct{}

func (m *mockError) Error() string { return "mock 失败" }

// ===== 命令参数边界(端口与目标形态) =====

// TestSCPCommand_默认端口 验证 SSH 端口 22/0/缺省(nil)均不带 -P 标志
// (scp 端口标志大写 -P,仅在非默认端口附加)。
func TestSCPCommand_默认端口(t *testing.T) {
	cases := []struct {
		name string
		srv  *conf.Server
	}{
		{"端口 22(显式)", fixtureServer()},
		{"端口 0(视为默认 22)", func() *conf.Server {
			s := fixtureServer()
			s.SSH.Port = 0
			return s
		}()},
		{"无 ssh 块", func() *conf.Server {
			s := fixtureServer()
			s.SSH = nil
			return s
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := scpCommand(tc.srv, "config.json")
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "-P") {
				t.Fatalf("默认端口不应带 -P 标志: %v", args)
			}
			if !strings.Contains(joined, "root@hk.example.com:"+RemoteDir+"/") {
				t.Fatalf("scp 目标缺远程目录: %v", args)
			}
		})
	}
}

// TestSSHCommand_端口0 验证 SSH.Port=0 视为默认 22,ssh 不带小写 -p 标志。
func TestSSHCommand_端口0(t *testing.T) {
	s := fixtureServer()
	s.SSH.Port = 0
	args := sshCommand(s, "echo hi")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-p") {
		t.Fatalf("端口 0 不应带 -p 标志: %v", args)
	}
	if !strings.Contains(joined, "root@hk.example.com") {
		t.Fatalf("ssh 目标缺失: %v", args)
	}
}

// TestSSHCommand_域名无括号 验证 sshTarget 仅对 IPv6 目标加括号,域名保持裸形态。
func TestSSHCommand_域名无括号(t *testing.T) {
	ipv6 := fixtureServer()
	ipv6.Address = "2001:db8::1"
	cases := []struct {
		name string
		srv  *conf.Server
		want string
	}{
		{"域名不加括号", fixtureServer(), "root@hk.example.com"},
		{"IPv6 加括号", ipv6, "root@[2001:db8::1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sshTarget(tc.srv); got != tc.want {
				t.Fatalf("sshTarget = %q, want %q", got, tc.want)
			}
		})
	}
}

// ===== Run(目录过滤 / 空目录 / nil 条目,mock) =====

// TestRun_跳过杂项文件 验证 os.ReadDir 后跳过子目录与 .DS_Store,
// scp 参数只含普通产物文件。
func TestRun_跳过杂项文件(t *testing.T) {
	s := fixtureServer()
	outDir := t.TempDir()
	prodDir := filepath.Join(outDir, "servers", "hk-01")
	if err := os.MkdirAll(prodDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(prodDir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	for _, f := range []string{"deploy.sh", "config.json", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(prodDir, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("写 %s: %v", f, err)
		}
	}
	var scpArgs string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		if name == "scp" {
			scpArgs = strings.Join(args, " ")
		}
		return nil
	}
	defer func() { runCmd = oldRunCmd }()

	inv := &conf.Inventory{Servers: []*conf.Server{s}}
	if err := Run(inv, outDir); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	for _, want := range []string{"deploy.sh", "config.json"} {
		if !strings.Contains(scpArgs, want) {
			t.Fatalf("scp 参数缺产物 %q: %s", want, scpArgs)
		}
	}
	for _, forbid := range []string{".DS_Store", "sub"} {
		if strings.Contains(scpArgs, forbid) {
			t.Fatalf("scp 参数不应含 %q: %s", forbid, scpArgs)
		}
	}
}

// TestRun_产物目录为空 验证目录存在但无产物时中止且不发起上传/部署。
//
// 注:Run 先执行"建远程目录"的 ssh 调用后才枚举产物,故 mock 计数为 1
// (仅建目录调用),scp 上传与远程 deploy.sh 均不会发生。
func TestRun_产物目录为空(t *testing.T) {
	s := fixtureServer()
	outDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outDir, "servers", "hk-01"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name)
		return nil
	}
	defer func() { runCmd = oldRunCmd }()

	inv := &conf.Inventory{Servers: []*conf.Server{s}}
	err := Run(inv, outDir)
	if err == nil || !strings.Contains(err.Error(), "产物目录为空") {
		t.Fatalf("期望产物目录为空错误,实际: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("调用数 = %d, want 1(仅建目录 ssh,不上传不部署): %v", len(calls), calls)
	}
}

// TestRun_nil条目跳过 验证清单含 nil 条目时跳过该台不 panic,只部署有效台。
func TestRun_nil条目跳过(t *testing.T) {
	outDir := t.TempDir()
	prodDir := filepath.Join(outDir, "servers", "hk-01")
	if err := os.MkdirAll(prodDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prodDir, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("写产物: %v", err)
	}
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name)
		return nil
	}
	defer func() { runCmd = oldRunCmd }()

	inv := &conf.Inventory{Servers: []*conf.Server{fixtureServer(), nil}}
	if err := Run(inv, outDir); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("调用数 = %d, want 3(仅部署 1 台,跳过 nil 条目): %v", len(calls), calls)
	}
}
