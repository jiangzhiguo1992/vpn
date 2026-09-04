// Package deploy OpenWrt 网关盒子部署测试:命令构造、host 解析与
// RunOpenWrt 编排(注入 mock,不实际连接),以及 OpenWrtScript 内容关键点。
//
// 复用 deploy_test.go 的 errMock/runCmd 注入模式,与清单部署同一口径。
package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ===== baseArgs(ssh/scp 公共参数) =====

// TestBaseArgs_端口形态 验证端口 22 不附加端口参数,非 22 按 portFlag
// 形态附加(-p/-P),超时与非交互参数恒在。
func TestBaseArgs_端口形态(t *testing.T) {
	// 默认端口 22:不带任何端口参数
	args := baseArgs(22, "-p")
	for _, forbid := range []string{"-p", "-P"} {
		if strings.Contains(strings.Join(args, " "), forbid) {
			t.Fatalf("端口 22 不应附加端口参数 %q: %v", forbid, args)
		}
	}
	// ssh 非 22:小写 -p
	args = baseArgs(2222, "-p")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "2222"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ssh 非默认端口缺 %q: %v", want, args)
		}
	}
	// scp 非 22:大写 -P(scp 的 -p 是 preserve-times,不可误用)
	args = baseArgs(2222, "-P")
	joined = strings.Join(args, " ")
	for _, want := range []string{"-P", "2222"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("scp 非默认端口缺 %q: %v", want, args)
		}
	}
	if strings.Contains(joined, " -p ") {
		t.Fatalf("scp 参数不应含小写 -p: %v", args)
	}
	// 超时保护与非交互三件套恒在
	for _, want := range []string{"ConnectTimeout=10", "ServerAliveInterval=15",
		"ServerAliveCountMax=3", "StrictHostKeyChecking=accept-new", "BatchMode=yes"} {
		if !strings.Contains(strings.Join(baseArgs(22, "-p"), " "), want) {
			t.Fatalf("baseArgs 缺 %q", want)
		}
	}
}

// ===== host 解析 =====

// TestParseOpenWrtHost_解析 user@host 各形态(user 缺省 root、IPv6 剥括号),
// 全部为合法输入,断言解析结果与 port 透传。
func TestParseOpenWrtHost_解析(t *testing.T) {
	cases := []struct {
		name     string
		hostArg  string
		wantUser string
		wantHost string
	}{
		{"带 user", "admin@192.168.1.1", "admin", "192.168.1.1"},
		{"裸 host user 缺省 root", "192.168.1.1", "root", "192.168.1.1"},
		{"v6 剥括号", "[2001:db8::1]", "root", "2001:db8::1"},
		{"带 user 的 v6", "admin@[2001:db8::1]", "admin", "2001:db8::1"},
		{"域名", "root@openwrt.lan", "root", "openwrt.lan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenWrtHost(tc.hostArg, 2222)
			if err != nil {
				t.Fatalf("parseOpenWrtHost(%q) 不应报错: %v", tc.hostArg, err)
			}
			if got.user != tc.wantUser || got.host != tc.wantHost {
				t.Fatalf("parseOpenWrtHost(%q) = %+v, want user=%q host=%q", tc.hostArg, got, tc.wantUser, tc.wantHost)
			}
			if got.port != 2222 {
				t.Fatalf("port = %d, want 2222", got.port)
			}
		})
	}
}

// TestParseOpenWrtHost_边界 验证 "@host" 形态 user 回退 root,以及多个 @、
// 空串、纯空白等非法目标返回 error(在发起任何远程动作前拦截)。
func TestParseOpenWrtHost_边界(t *testing.T) {
	cases := []struct {
		name     string
		hostArg  string
		wantUser string
		wantHost string
		wantErr  bool
	}{
		{"@host user 回退 root", "@host", "root", "host", false},
		{"多个 @ 报错", "a@b@c", "", "", true},
		{"空串报错", "", "", "", true},
		{"纯空白报错", "   ", "", "", true},
		{"裸 @ 报错", "@", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenWrtHost(tc.hostArg, 2222)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseOpenWrtHost(%q) 应报错,实际返回 %+v", tc.hostArg, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOpenWrtHost(%q) 不应报错: %v", tc.hostArg, err)
			}
			if got.user != tc.wantUser || got.host != tc.wantHost {
				t.Fatalf("parseOpenWrtHost(%q) = %+v, want user=%q host=%q", tc.hostArg, got, tc.wantUser, tc.wantHost)
			}
			if got.port != 2222 {
				t.Fatalf("port = %d, want 2222", got.port)
			}
		})
	}
}

// ===== 命令构造 =====

// TestOpenWrtSSHCommand_参数 验证目标组合与端口形态(含 IPv6 加回括号)。
func TestOpenWrtSSHCommand_参数(t *testing.T) {
	cases := []struct {
		name   string
		t      openWrtTarget
		cmd    string
		want   []string
		forbid []string
	}{
		{"默认端口", openWrtTarget{"root", "192.168.1.1", 22}, "echo hi",
			[]string{"root@192.168.1.1", "echo hi"}, []string{"-p"}},
		{"非 22 端口加 -p", openWrtTarget{"root", "192.168.1.1", 2222}, "x",
			[]string{"-p", "2222", "root@192.168.1.1"}, nil},
		{"IPv6 加回括号", openWrtTarget{"admin", "2001:db8::1", 22}, "x",
			[]string{"admin@[2001:db8::1]"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := openWrtSSHCommand(tc.t, tc.cmd)
			joined := strings.Join(args, " ")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Fatalf("ssh 参数缺 %q: %v", w, args)
				}
			}
			for _, f := range tc.forbid {
				if strings.Contains(joined, f) {
					t.Fatalf("ssh 参数不应含 %q: %v", f, args)
				}
			}
			// 远程命令必须是最后一个参数(多行脚本直传不拆)
			if args[len(args)-1] != tc.cmd {
				t.Fatalf("远程命令应为末位参数: %v", args)
			}
		})
	}
}

// TestOpenWrtSCPCommand_参数 验证 scp 端口标志大写 -P 且远端落点固定
// /tmp/sing-box-openwrt.json(不依赖源文件名)。
func TestOpenWrtSCPCommand_参数(t *testing.T) {
	cases := []struct {
		name   string
		t      openWrtTarget
		srcs   []string
		want   []string
		forbid []string
	}{
		{"默认端口落点固定文件名", openWrtTarget{"root", "192.168.1.1", 22},
			[]string{"sing-box-openwrt.json"},
			[]string{"sing-box-openwrt.json", "root@192.168.1.1:/tmp/sing-box-openwrt.json"}, []string{"-P"}},
		{"非 22 端口加 -P", openWrtTarget{"root", "192.168.1.1", 2222},
			[]string{"dist/whatever.json"},
			[]string{"-P", "2222", "dist/whatever.json", "root@192.168.1.1:/tmp/sing-box-openwrt.json"}, nil},
		{"IPv6 目标加回括号", openWrtTarget{"root", "2001:db8::1", 22},
			[]string{"f.json"}, []string{"root@[2001:db8::1]:/tmp/sing-box-openwrt.json"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := openWrtSCPCommand(tc.t, tc.srcs...)
			joined := strings.Join(args, " ")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Fatalf("scp 参数缺 %q: %v", w, args)
				}
			}
			for _, f := range tc.forbid {
				if strings.Contains(joined, f) {
					t.Fatalf("scp 参数不应含 %q: %v", f, args)
				}
			}
			if args[len(args)-1] != tc.t.target()+":/tmp/sing-box-openwrt.json" {
				t.Fatalf("scp 末位参数应为远程固定落点 /tmp/sing-box-openwrt.json: %v", args)
			}
		})
	}
}

// ===== RunOpenWrt(注入 mock) =====

// TestRunOpenWrt_成功序列 验证调用序列为 scp → ssh(先上传后执行),
// scp 源为本地产物、ssh 末位参数为 OpenWrtScript 全量。
func TestRunOpenWrt_成功序列(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sing-box-openwrt.json")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("写产物: %v", err)
	}
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	defer func() { runCmd = oldRunCmd }()

	if err := RunOpenWrt("root@192.168.1.1", 22, cfgPath); err != nil {
		t.Fatalf("RunOpenWrt 失败: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("调用数 = %d, want 2(scp → ssh)\n%v", len(calls), calls)
	}
	if !strings.HasPrefix(calls[0], "scp ") || !strings.Contains(calls[0], "root@192.168.1.1:/tmp/sing-box-openwrt.json") {
		t.Fatalf("第一步应为上传配置到 /tmp/sing-box-openwrt.json: %s", calls[0])
	}
	if !strings.HasPrefix(calls[1], "ssh ") {
		t.Fatalf("第二步应为 ssh: %s", calls[1])
	}
	// ssh 末位参数是整段 OpenWrtScript(set -eu 开头,原样直传不经本地 shell)
	if !strings.HasSuffix(calls[1], OpenWrtScript) || !strings.Contains(calls[1], "set -eu") {
		t.Fatalf("ssh 应直传 OpenWrtScript 全文: %s...", calls[1][:min(80, len(calls[1]))])
	}
}

// TestRunOpenWrt_产物缺失 验证未 gen 时给出引导错误且不发起任何调用。
func TestRunOpenWrt_产物缺失(t *testing.T) {
	var calls []string
	oldRunCmd := runCmd
	runCmd = func(name string, args []string) error {
		calls = append(calls, name)
		return nil
	}
	defer func() { runCmd = oldRunCmd }()
	err := RunOpenWrt("192.168.1.1", 22, filepath.Join(t.TempDir(), "sing-box-openwrt.json"))
	if err == nil || !strings.Contains(err.Error(), "先 make gen") {
		t.Fatalf("期望产物缺失引导错误,实际: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("产物缺失不应发起远程调用: %v", calls)
	}
}

// TestRunOpenWrt_失败包装 验证 scp/ssh 任一步失败立即返回且错误含目标地址。
func TestRunOpenWrt_失败包装(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sing-box-openwrt.json")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("写产物: %v", err)
	}
	cases := []struct {
		name       string
		failOn     string // 命令名(或空=不失败)
		wantPhrase string
	}{
		{"scp 失败", "scp", "上传配置失败"},
		{"ssh 失败", "ssh", "远程部署失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldRunCmd := runCmd
			runCmd = func(name string, args []string) error {
				if name == tc.failOn {
					return errMock
				}
				return nil
			}
			defer func() { runCmd = oldRunCmd }()
			err := RunOpenWrt("admin@[2001:db8::1]", 2222, cfgPath)
			if err == nil {
				t.Fatal("期望失败,实际 nil")
			}
			if !strings.Contains(err.Error(), "admin@[2001:db8::1]") {
				t.Fatalf("错误应含目标地址: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Fatalf("错误应含 %q: %v", tc.wantPhrase, err)
			}
			// H2:scp 失败(常见于 OpenSSH 9+ 走 SFTP 而盒子缺 sftp-server)
			// 时错误须带安装引导
			if tc.failOn == "scp" && !strings.Contains(err.Error(), "openssh-sftp-server") {
				t.Fatalf("scp 失败错误应含 openssh-sftp-server 引导: %v", err)
			}
		})
	}
}

// ===== OpenWrtScript 内容关键点 =====

// TestOpenWrtScript_内容 断言远端脚本含全部关键步骤(自动安装/装后复检/
// UCI/init 集成检查/校验/备份/落位/启动锚/DNS 让位/进程验证),防脚本被
// 误裁剪。
func TestOpenWrtScript_内容(t *testing.T) {
	for _, want := range []string{
		"set -eu",
		"chmod 600 /tmp/sing-box-openwrt.json",
		"缺少 UCI 配置 sing-box.main 或 init 脚本",
		"安装后 sing-box 仍不可用",
		"opkg install --force-reinstall sing-box",
		"apk fix sing-box",
		"apk update",
		"apk add sing-box ip-full",
		"opkg update",
		"opkg install sing-box ip-full",
		"sing-box check -c /tmp/sing-box-openwrt.json",
		"sing-box version",
		"docs/openwrt.md 升级内核",
		"config.json.bak",
		"dhcp.bak",
		"sing-box.bak",
		"保留首次部署前备份",
		"rm -f /tmp/sing-box-openwrt.json",
		"uci set sing-box.main.enabled='1'",
		"uci set sing-box.main.user='root'",
		"uci commit sing-box",
		"/etc/init.d/sing-box enable",
		"/etc/init.d/sing-box restart",
		"if ! uci get dhcp.@dnsmasq[0] >/dev/null 2>&1",
		"uci set dhcp.@dnsmasq[0].port='0'",
		"uci commit dhcp",
		"pgrep -x dnsmasq",
		"/etc/init.d/dnsmasq restart",
		"pgrep -f 'sing-box run'",
		"启动后退出",
		"logread -e sing-box",
		"稍后重跑一次即可",
	} {
		if !strings.Contains(OpenWrtScript, want) {
			t.Fatalf("OpenWrtScript 缺关键内容 %q", want)
		}
	}
	// A1 锚点法关键文本(时间戳唯一锚 + logger + awk -v 注入,含引号单独断言)
	for _, want := range []string{
		"anchor=\"deploy-restart-begin-$(date +%s)\"",
		"logger -t sing-box \"$anchor\"",
		"awk -v a=\"$anchor\" 'index($0,a){f=1} f && tolower($0) ~ /fatal/{print \"1\"; exit}'",
	} {
		if !strings.Contains(OpenWrtScript, want) {
			t.Fatalf("OpenWrtScript 缺 A1 锚点法关键内容 %q", want)
		}
	}
	// 次序断言(防步骤挪位破坏 A2/A1 修复语义与回滚安全次序):
	//  1. 自动安装(2/9)先于 UCI/init 集成检查(3/9):全新盒子(UCI/init/
	//     二进制皆无)先装好二进制再查集成,装后复检也须在 3/9 之前
	//  2. UCI/init 检查先于 check/备份/落位等写操作;落位 cp 先于 rm
	//  3. 日志锚先于 restart(供验证段区分本次启动);dnsmasq 让位在
	//     restart 后、进程验证之前
	idxInstall := strings.Index(OpenWrtScript, "apk add sing-box ip-full")
	idxRecheck := strings.Index(OpenWrtScript, "安装后 sing-box 仍不可用")
	idxUCI := strings.Index(OpenWrtScript, "缺少 UCI 配置 sing-box.main 或 init 脚本")
	idxCheck := strings.Index(OpenWrtScript, "sing-box check")
	idxBackup := strings.Index(OpenWrtScript, "已备份 /etc/sing-box/config.json")
	idxCP := strings.Index(OpenWrtScript, "cp /tmp/sing-box-openwrt.json /etc/sing-box/config.json")
	idxRM := strings.Index(OpenWrtScript, "rm -f /tmp/sing-box-openwrt.json")
	idxAnchor := strings.Index(OpenWrtScript, "deploy-restart-begin")
	idxRestart := strings.Index(OpenWrtScript, "/etc/init.d/sing-box restart")
	idxPort := strings.Index(OpenWrtScript, "uci set dhcp.@dnsmasq[0].port='0'")
	idxVerify := strings.Index(OpenWrtScript, "pgrep -f 'sing-box run'")
	for _, i := range []int{idxInstall, idxRecheck, idxUCI, idxCheck, idxBackup, idxCP, idxRM, idxAnchor, idxRestart, idxPort, idxVerify} {
		if i < 0 {
			t.Fatalf("次序断言锚点缺失(idx=%d)", i)
		}
	}
	if !(idxInstall < idxRecheck && idxRecheck < idxUCI && idxUCI < idxCheck && idxCheck < idxBackup && idxBackup < idxCP && idxCP < idxRM) {
		t.Fatal("执行次序错误:应 自动安装 → 装后复检 → UCI/init 检查 → check → 备份 → 落位 cp → 删 /tmp 上传件")
	}
	if !(idxRM < idxAnchor && idxAnchor < idxRestart) {
		t.Fatal("日志锚应位于 restart 之前(且落位/清理之后)")
	}
	if !(idxRestart < idxPort && idxPort < idxVerify) {
		t.Fatal("执行次序错误:应 restart → dnsmasq 让位 → 进程验证")
	}
	if !strings.Contains(OpenWrtScript, "! -f /etc/sing-box/config.json.bak") {
		t.Fatal("config.json 备份分支应含幂等保护(! -f 判 .bak 存在)")
	}
}

// TestOpenWrtScript_sh语法 用本机 sh/dash 对 OpenWrtScript 做 sh -n 语法
// 校验(写临时文件执行,不污染仓库);本机缺 sh 则跳过。
func TestOpenWrtScript_sh语法(t *testing.T) {
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("本机缺 sh,跳过 OpenWrtScript 语法校验: %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "openwrt.sh")
	if err := os.WriteFile(scriptPath, []byte(OpenWrtScript), 0o600); err != nil {
		t.Fatalf("写临时脚本: %v", err)
	}
	if out, err := exec.Command(shPath, "-n", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("sh -n 语法校验失败: %v\n%s", err, out)
	}
}
