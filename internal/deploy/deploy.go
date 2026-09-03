// Package deploy 部署执行:读清单 → 逐台 ssh 建目录 + scp 上传 +
// 远程执行 deploy.sh,一条命令部署全部服务器。
//
// 设计决策:
//   - SSH 目标复用清单 address(连接地址与 SSH 目标同主机),user 缺省
//     root、端口缺省 22
//   - 命令构造纯函数化(sshCommand/scpCommand 返回 exec args,不含
//     argv0),便于单测断言参数形态,不实际执行
//   - 错误即停:任一台失败立即返回(含服务器名),避免半自动部署静默
//     继续导致多台半成品;deploy.sh 幂等,修复后重跑安全
//   - stdout/stderr 透传:ssh/scp 输出直接展示(进度 + 远程输出)
//   - ssh/scp 三件套:ConnectTimeout 防挂起、StrictHostKeyChecking=
//     accept-new 首次自动接受指纹、BatchMode=yes 非交互(未配密钥
//     立即失败而非卡等密码);ServerAlive 防空闲超时掐断长任务
//   - scp 只增不删,上传前先删远程 cert.sh(清单动态产物;清单从
//     「有 H2」改「无 H2」后本地 cert.sh 已被 WriteArtifacts 删除,
//     但远程残留会让 deploy.sh 误执行过时脚本)
//   - IPv6 地址 ssh/scp 目标加方括号(scp 按首个冒号截断 host)
//
// 职责边界:仅执行部署(远程命令);产物生成在 internal/server;
// 清单模型在 internal/conf。
//
// 使用示例:
//
//	err := deploy.Run(inv, "dist")
package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"vpn/internal/conf"
)

// RemoteDir 是服务器上部署产物目录(与远程 deploy.sh 执行路径一致)。
const RemoteDir = "/opt/sing-box"

// Run 执行部署:读清单 → 逐台 ssh 建目录 + scp 上传 + 远程执行 deploy.sh。
//
// 调用说明:先 gen 生成产物(outDir/servers/<name>/);本函数逐台自动化,
// 任一台失败立即返回。参数 inv:已加载清单;outDir:产物根目录(同 gen
// 的 -out)。返回:全部成功 nil;中途失败返回含服务器名的包装错误。
func Run(inv *conf.Inventory, outDir string) error {
	for _, s := range inv.Servers {
		if s == nil {
			continue
		}
		target := sshTarget(s)
		dir := filepath.Join(outDir, "servers", s.Name)
		if _, err := os.Stat(dir); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("deploy.Run: 服务器 %q 产物不存在(先 make gen): %s", s.Name, dir)
			}
			return fmt.Errorf("deploy.Run: 服务器 %q 读取产物目录失败: %s: %w", s.Name, dir, err)
		}
		fmt.Printf("==> 部署 %s(%s)...\n", s.Name, target)
		// 1. 建远程目录(/opt 属 root,需 sudo mkdir + chown 归部署用户
		//    使 scp 可写;root 直过,无 sudo 的极简系统兜底直接执行)
		mkdirCmd := "if command -v sudo >/dev/null 2>&1; then sudo -n mkdir -p " + RemoteDir +
			" && sudo -n chown $(id -un):$(id -un) " + RemoteDir +
			"; else mkdir -p " + RemoteDir + " && chown $(id -un):$(id -un) " + RemoteDir + "; fi" +
			" && rm -f " + RemoteDir + "/cert.sh"
		if err := runCmd("ssh", sshCommand(s, mkdirCmd)); err != nil {
			return fmt.Errorf("deploy.Run: 服务器 %q 建远程目录失败: %w", s.Name, err)
		}
		// 2. 上传产物(deploy.sh 已含装 Docker → 证书 → 启动 → 验证)。
		//    os/exec 不经 shell,scp 通配符不展开,os.ReadDir 列出逐个传
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("deploy.Run: 服务器 %q 枚举产物文件失败: %w", s.Name, err)
		}
		files := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || e.Name() == ".DS_Store" {
				continue // 跳过子目录与 macOS Finder 元数据
			}
			files = append(files, filepath.Join(dir, e.Name()))
		}
		if len(files) == 0 {
			return fmt.Errorf("deploy.Run: 服务器 %q 产物目录为空(先 make gen): %s", s.Name, dir)
		}
		if err := runCmd("scp", scpCommand(s, files...)); err != nil {
			return fmt.Errorf("deploy.Run: 服务器 %q 上传产物失败: %w", s.Name, err)
		}
		// 3. 远程一键部署
		if err := runCmd("ssh", sshCommand(s, "cd "+RemoteDir+" && sh deploy.sh")); err != nil {
			return fmt.Errorf("deploy.Run: 服务器 %q 远程部署失败: %w", s.Name, err)
		}
		fmt.Printf("✓ %s 部署完成\n", s.Name)
	}
	return nil
}

// sshTarget 构造 <user>@<host>(user 缺省 root)。
func sshTarget(s *conf.Server) string {
	user := "root"
	if s.SSH != nil && s.SSH.User != "" {
		user = s.SSH.User
	}
	return user + "@" + sshHost(s.Address)
}

// sshCommand 构造 ssh 命令参数(不含 argv0,供 exec.Command("ssh", args...))。
//
// 调用说明:ssh 端口参数小写 -p,仅在非 22 时附加(见 sshBaseArgs)。
// 参数 s:清单条目(SSH 配置可 nil);remoteCmd:远程执行的 shell 命令。
// 返回:exec.Command 的 args。
func sshCommand(s *conf.Server, remoteCmd string) []string {
	args := sshBaseArgs(s, "-p")
	args = append(args, sshTarget(s), remoteCmd)
	return args
}

// scpCommand 构造 scp 上传命令参数(不含 argv0,目标目录 RemoteDir)。
//
// 调用说明:scp 端口参数大写 -P(-p 在 scp 语义是 preserve-times);
// 源为多个文件参数(调用方已展开目录,os/exec 不经 shell)。
// 参数 s:清单条目;srcFiles:本地产物文件列表。
// 返回:exec.Command 的 args。
func scpCommand(s *conf.Server, srcFiles ...string) []string {
	args := sshBaseArgs(s, "-P")
	args = append(args, srcFiles...)
	args = append(args, sshTarget(s)+":"+RemoteDir+"/")
	return args
}

// sshHost 构造 SSH 目标主机(IPv6 加方括号,防 scp 按冒号截断 host)。
func sshHost(host string) string {
	if strings.Contains(host, ":") { // IPv6(域名不可能含冒号)
		return "[" + host + "]"
	}
	return host
}

// sshBaseArgs 返回 ssh/scp 公共基础参数:非默认端口附加端口参数 + 超时
// 保护 + 非交互三件套。
//
// 调用说明:ssh 与 scp 端口参数形态不同(ssh -p / scp -P),由调用方传
// portFlag。端口 0/22 视为默认不附加。
func sshBaseArgs(s *conf.Server, portFlag string) []string {
	args := make([]string, 0, 10)
	port := 22
	if s.SSH != nil && s.SSH.Port != 0 {
		port = s.SSH.Port
	}
	if port != 22 {
		args = append(args, portFlag, strconv.Itoa(port))
	}
	args = append(args,
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	)
	return args
}

// runCmd 执行外部命令(ssh/scp)并透传 stdout/stderr。
//
// 调用说明:部署需要交互式输出(进度、远程输出),直接透传终端。
// 参数 name:命令名(ssh/scp);args:exec args(不含 argv0)。
// 返回:命令执行失败时返回错误(含命令上下文与退出码)。
// 包级变量(非 func 声明):测试可注入 mock(见 deploy_test.go)。
var runCmd = func(name string, args []string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
