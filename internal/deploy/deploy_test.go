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
