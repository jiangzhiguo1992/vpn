// OpenWrt 网关盒子一键部署子命令(openwrt):把 gen 生成的
// sing-box-openwrt.json 部署到单台 OpenWrt 网关。
//
// 用途:局域网接入 OpenWrt 网关盒子(官方源装裸 sing-box)做全屋代理时,
// 一条命令完成 scp 上传 + 远端部署(装包/校验/备份/UCI root 运行/dnsmasq
// DNS 让位),与 deploy 区别:目标单台且不入清单,直接 -host 指定。
//
// 设计决策:
//   - 独立 flag 集,无全局 flag 污染(与 gen/deploy 同风格);-out 默认
//     dist,与 gen 的 -out 一致
//   - 本地产物文件名固定 sing-box-openwrt.json(cmd/vpn gen 写盘),
//     与远端脚本 /tmp 落点同名约定
//
// 使用示例:
//
//	go run ./cmd/vpn gen -inventory servers.json -out dist
//	go run ./cmd/vpn openwrt -host root@192.168.1.1 -p 22 -out dist
package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"vpn/internal/deploy"
)

// runOpenWrt 执行 OpenWrt 一键部署:解析 -host/-p/-out 后委托
// deploy.RunOpenWrt(产物 = <out>/sing-box-openwrt.json)。
func runOpenWrt(args []string) error {
	fs := flag.NewFlagSet("openwrt", flag.ExitOnError)
	host := fs.String("host", "", "OpenWrt 网关 SSH 目标(可带 user@,缺省 root),如 root@192.168.1.1")
	port := fs.Int("p", 22, "SSH 端口(默认 22)")
	outDir := fs.String("out", "dist", "产物输出目录(与 gen 的 -out 一致)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// 参数校验前置于任何部署动作:空 host(含纯空白)与越界端口直接拒绝,
	// 在调用 deploy 前返回,避免半参数打到真实网关
	hostArg := strings.TrimSpace(*host)
	if hostArg == "" {
		return fmt.Errorf("openwrt: 缺少必填参数 -host <user@host>(先 make gen 生成产物)")
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("openwrt: -p 端口须在 1..65535(实际 %d)", *port)
	}
	if err := deploy.RunOpenWrt(hostArg, *port, filepath.Join(*outDir, "sing-box-openwrt.json")); err != nil {
		return fmt.Errorf("openwrt: %w", err)
	}
	return nil
}
