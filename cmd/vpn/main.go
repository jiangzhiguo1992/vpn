// Command vpn 是项目入口:gen(生成全部产物)/ deploy(一键部署)/
// openwrt(OpenWrt 网关盒子一键部署)/ doctor(环境自检)。
//
// 用途:一条命令走完整闭环:填写清单(servers.json)→ gen 生成服务端
// 产物与客户端产物 → deploy 上传并远程部署全部服务器 → 用客户端产物
// 在任意设备导入上网;家里有 OpenWrt 网关盒子时,vpn openwrt 把
// sing-box-openwrt.json 一键部署到盒子做全局代理。
//
// 设计决策:
//   - gen 顺序:Load → Validate → Backfill(凭据回填)→ 逐台生成服务端
//     产物并收集节点 → 渲染客户端产物 → 统一保存清单(幂等:再次 gen
//     复用已有凭据,产物不变);中途失败也尽力保存已回填凭据(重跑
//     复用,不会密钥翻新导致已分发客户端失效)
//   - 产物文件权限:含凭据的(config.json/links.txt/sub.txt/
//     sing-box*.json/清单)一律 0600,仅属主可读写;编排与脚本文件
//     0644/0755 由各生成函数决定
//   - 子命令独立 flag 集,无全局 flag 污染
//
// 职责边界:编排与产物写盘;渲染在 internal/server 与 internal/client;
// 部署执行在 internal/deploy;清单模型在 internal/conf。
//
// 使用示例:
//
//	go run ./cmd/vpn gen -inventory servers.json -out dist
//	go run ./cmd/vpn deploy -inventory servers.json -out dist
//	go run ./cmd/vpn openwrt -host root@192.168.1.1 -out dist
//	go run ./cmd/vpn doctor
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"vpn/internal/client"
	"vpn/internal/conf"
	"vpn/internal/deploy"
	"vpn/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = runGen(os.Args[2:])
	case "deploy":
		err = runDeploy(os.Args[2:])
	case "openwrt":
		err = runOpenWrt(os.Args[2:])
	case "doctor":
		err = runDoctor()
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "未知子命令 %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

// usage 打印命令行帮助。
func usage() {
	fmt.Print(`vpn 是自建 VPN 方案的生成与部署入口。

用法:
  vpn gen    -inventory <清单> -out <产物目录>   生成服务端与客户端全部产物
  vpn deploy -inventory <清单> -out <产物目录>   上传并远程部署全部服务器
  vpn openwrt -host <user@host> [-p <port>] [-out <dir>]   OpenWrt 网关盒子一键部署
  vpn doctor                                     环境自检(go/ssh/scp/openssl)

清单与产物说明见 README.md 与 docs/server.md。
`)
}

// runGen 执行 gen:载入清单 → 校验 → 回填 → 生成全部产物 → 保存清单。
func runGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	invPath := fs.String("inventory", "servers.json", "服务器清单 JSON 路径")
	outDir := fs.String("out", "dist", "产物输出目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inv := &conf.Inventory{}
	if err := conf.LoadFile(*invPath, inv); err != nil {
		return fmt.Errorf("加载清单 %s 失败(首次使用请 cp example-servers.json %s 并填写): %w", *invPath, *invPath, err)
	}
	if err := inv.Validate(); err != nil {
		return fmt.Errorf("清单校验失败: %w", err)
	}
	if err := inv.Backfill(); err != nil {
		return fmt.Errorf("凭据回填失败: %w", err)
	}
	// 失败也尽力持久化已回填凭据(重跑复用,避免中途失败后全部重新生成)
	fail := func(err error) error {
		if saveErr := conf.SaveFile(*invPath, inv); saveErr != nil {
			return fmt.Errorf("%w(另:保存已回填凭据失败: %v)", err, saveErr)
		}
		return err
	}
	nodes := make([]conf.Node, 0, len(inv.Servers)*3)
	for _, s := range inv.Servers {
		if err := server.WriteArtifacts(filepath.Join(*outDir, "servers", s.Name), s); err != nil {
			return fail(err)
		}
		nodes = append(nodes, s.Nodes()...)
	}
	if err := writeClientArtifacts(*outDir, nodes); err != nil {
		return fail(err)
	}
	if err := conf.SaveFile(*invPath, inv); err != nil {
		return err
	}
	printSummary(*outDir, inv)
	return nil
}

// writeClientArtifacts 渲染并写盘客户端产物(links/sub 与 sing-box 全产物,
// 全部含节点凭据,0600)。
func writeClientArtifacts(outDir string, nodes []conf.Node) error {
	if len(nodes) == 0 {
		return fmt.Errorf("无客户端节点可导出(清单为空)")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("创建产物目录 %s: %w", outDir, err)
	}
	links, err := client.RenderLinks(nodes)
	if err != nil {
		return err
	}
	sub, err := client.RenderSub(nodes)
	if err != nil {
		return err
	}
	singBoxJSON, err := client.RenderSingBox(nodes)
	if err != nil {
		return err
	}
	// 桌面 GUI 全接管变体(官方桌面客户端均为纯内核,需 tun 才接管系统流量):
	// SFM(macOS)/SFW(Windows)/SFL(Linux),见 internal/client/singbox.go
	singBoxSFMJSON, err := client.RenderSingBoxSFM(nodes)
	if err != nil {
		return err
	}
	singBoxSFWJSON, err := client.RenderSingBoxSFW(nodes)
	if err != nil {
		return err
	}
	singBoxSFLJSON, err := client.RenderSingBoxSFL(nodes)
	if err != nil {
		return err
	}
	// OpenWrt 网关盒子变体(裸 sing-box TUN 全接管,配合 vpn openwrt 部署),
	// 见 internal/client/singbox.go
	singBoxOpenWrtJSON, err := client.RenderSingBoxOpenWrt(nodes)
	if err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{"links.txt", []byte(links)},
		{"sub.txt", []byte(sub)},
		{"sing-box.json", append(singBoxJSON, '\n')},
		{"sing-box-sfm.json", append(singBoxSFMJSON, '\n')},
		{"sing-box-sfw.json", append(singBoxSFWJSON, '\n')},
		{"sing-box-sfl.json", append(singBoxSFLJSON, '\n')},
		{"sing-box-openwrt.json", append(singBoxOpenWrtJSON, '\n')},
	}
	for _, f := range files {
		if err := writeFile0600(filepath.Join(outDir, f.name), f.data); err != nil {
			return err
		}
	}
	return nil
}

// writeFile0600 以 0600 权限写文件(显式 Chmod,防旧权限残留)。
func writeFile0600(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写 %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("设置 %s 权限 0600: %w", path, err)
	}
	return nil
}

// printSummary 输出产物清单(用户按图索骥取用)。
func printSummary(outDir string, inv *conf.Inventory) {
	var sb strings.Builder
	sb.WriteString("生成完成,产物目录: " + outDir + "\n")
	for _, s := range inv.Servers {
		sb.WriteString(fmt.Sprintf("  servers/%s/  服务端部署产物(部署到 %s)\n", s.Name, s.Address))
	}
	sb.WriteString("  links.txt      全部节点分享链接(剪贴板/扫码导入任意客户端)\n")
	sb.WriteString("  sub.txt        通用订阅(支持订阅导入的客户端)\n")
	sb.WriteString("  sing-box.json      sing-box 官方客户端通用版(SFI/SFA/CLI)\n")
	sb.WriteString("  sing-box-sfm.json  sing-box 官方桌面 SFM 版(macOS TUN 全接管)\n")
	sb.WriteString("  sing-box-sfw.json  sing-box 官方桌面 SFW 版(Windows 连接自动设系统代理)\n")
	sb.WriteString("  sing-box-sfl.json  sing-box 官方桌面 SFL 版(Linux TUN 全接管)\n")
	sb.WriteString("  sing-box-openwrt.json  sing-box 官方 OpenWrt 版(网关盒子 TUN 全接管,配合 vpn openwrt 部署)\n")
	fmt.Print(sb.String())
}

// runDeploy 执行 deploy:载入清单 → 校验 → 逐台上传部署。
func runDeploy(args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	invPath := fs.String("inventory", "servers.json", "服务器清单 JSON 路径")
	outDir := fs.String("out", "dist", "产物输出目录(与 gen 的 -out 一致)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inv := &conf.Inventory{}
	if err := conf.LoadFile(*invPath, inv); err != nil {
		return err
	}
	if err := inv.Validate(); err != nil {
		return fmt.Errorf("清单校验失败: %w", err)
	}
	return deploy.Run(inv, *outDir)
}

// runDoctor 执行环境自检(go 版本 + ssh/scp/openssl 可用性)。
func runDoctor() error {
	fmt.Printf("系统: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	// go 版本(go.mod 声明 go 1.27,低于 1.27 无法编译本项目)
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return fmt.Errorf("doctor: go 不可用(需 Go 1.27+): %w", err)
	}
	ver := strings.TrimSpace(string(out))
	fmt.Printf("go: %s\n", ver)
	if !goVersionAtLeast(ver, 1, 27) {
		return fmt.Errorf("doctor: 需要 Go 1.27+(当前 %s)", ver)
	}
	// ssh/scp(部署需要;Windows 10 1809+ 内置 OpenSSH 客户端)
	for _, bin := range []string{"ssh", "scp"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("doctor: %s 不可用(deploy 需要;Windows 请启用 OpenSSH 客户端可选功能)", bin)
		}
	}
	// openssl(cert.sh 生成 H2 自签证书需要;部署到服务器执行,本机可选)
	if _, err := exec.LookPath("openssl"); err == nil {
		fmt.Println("openssl: 可用(服务器侧 cert.sh 需要,本机缺失不影响部署)")
	} else {
		fmt.Println("openssl: 本机缺失(无碍:证书在服务器上生成,服务器需有 openssl)")
	}
	fmt.Println("环境自检通过")
	return nil
}

// goVersionAtLeast 判读 go version 输出是否 >= major.minor。
// 版本串形如 "go1.27.0 darwin/arm64",忽略 patch 与后缀。
func goVersionAtLeast(ver string, major, minor int) bool {
	m := goVersionRe.FindStringSubmatch(ver)
	if len(m) != 3 {
		return false
	}
	vMajor, err1 := strconv.Atoi(m[1])
	vMinor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return false
	}
	if vMajor != major {
		return vMajor > major
	}
	return vMinor >= minor
}

// goVersionRe 提取 go version 的主次版本号。
var goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)`)
