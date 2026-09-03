// Package client 客户端产物生成:分享链接 + 各客户端配置。
//
// 用途:由全部节点(conf.Node,清单回填后反推)渲染客户端分发物:
//   - links.txt     每行一个分享链接(vless:// ss:// hysteria2://),
//     剪贴板/扫码/手动添加,覆盖一切支持链接导入的客户端
//   - sub.txt       通用订阅(base64 编码的全部链接,机场标准格式),
//     支持订阅的客户端直接使用;也可作为自托管订阅源的内容
//   - clash.yaml    Clash 系(mihomo/Clash Verge Rev/OpenClash 等)订阅,
//     含代理组与国内直连分流规则
//   - sing-box.json sing-box 官方客户端(桌面 CLI/SFI/SFA)完整配置,
//     含 mixed 入站/自动选择组/基础路由
//
// 设计决策:
//   - 节点渲染全部从 conf.Node 单一来源,与服务端 config.json 同源
//     同凭据,产物之间不可能漂移(防凭据漂移测试见 *_test.go)
//   - 字符串值统一经 JSON 转义后进入模板(密码/SNI 防注入),渲染后
//     语法校验双保险(缺占位符静默产出 <no value>)
//   - 分流规则做"开箱即用"取舍:clash 用 GEOSITE/GEOIP 关键词(主流
//     GUI 客户端自带 geodata);sing-box.json 用全局代理 + 内网直连
//     形态(不依赖 .srs 外置规则文件,官方 app 导入即用),需要国内
//     分流的用户按 docs/clients/sing-box.md 自行追加
//
// 职责边界:纯渲染,零文件 I/O;节点模型在 internal/conf;
// 产物写盘与编排在 cmd/vpn。
//
// 使用示例:
//
//	nodes := srv.Nodes() // 每台服务器收集后合并
//	link := client.ShareLink(nodes[0])
//	data, _ := client.RenderSingBox(nodes)
package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	"vpn/internal/conf"
)

// ShareLink 生成单节点分享链接(按协议分派;未知类型返回空串)。
//
// 调用说明:节点须来自已回填清单(凭据非空);Name 空时用 Address:Port
// 兜底(fragment 是客户端显示名,无凭据泄露风险)。
func ShareLink(n conf.Node) string {
	switch n.Type {
	case conf.TypeVLESSReality:
		return vlessShareLink(n)
	case conf.TypeShadowsocks:
		return ssShareLink(n)
	case conf.TypeHysteria2:
		return hysteria2ShareLink(n)
	default:
		return ""
	}
}

// vlessShareLink 组装 vless:// Reality 链接。
//
// 形态:vless://<uuid>@<host>:<port>?type=tcp&security=reality&encryption=
// none&fp=chrome&sni=&pbk=&sid=&flow=xtls-rprx-vision#<name>(sni/pbk/sid
// 空省略;flow 恒 xtls-rprx-vision)。
func vlessShareLink(n conf.Node) string {
	query := url.Values{
		"type":       {"tcp"},
		"security":   {"reality"},
		"encryption": {"none"},
		"fp":         {"chrome"},
		"flow":       {"xtls-rprx-vision"},
	}
	if n.ServerName != "" {
		query.Set("sni", n.ServerName)
	}
	if n.PublicKey != "" {
		query.Set("pbk", n.PublicKey)
	}
	if n.ShortID != "" {
		query.Set("sid", n.ShortID)
	}
	return fmt.Sprintf("vless://%s@%s?%s#%s", n.UUID, shareHost(n),
		query.Encode(), url.PathEscape(shareFragment(n)))
}

// ssShareLink 组装 ss:// 链接(SIP002 形态)。
//
// 形态:ss://<base64(method:password)>@<host>:<port>#<name>;userinfo 用
// 标准 base64 带 padding(SIP002 规范写法,主流客户端 sing-box/mihomo/
// hiddify/移动端 app 均兼容)。
func ssShareLink(n conf.Node) string {
	userinfo := base64StdEncode([]byte(n.Method + ":" + n.Password))
	return fmt.Sprintf("ss://%s@%s#%s", userinfo, shareHost(n),
		url.PathEscape(shareFragment(n)))
}

// hysteria2ShareLink 组装 hysteria2:// 链接。
//
// 形态:hysteria2://<auth>@<host>:<port>/?insecure=0|1&sni=...#<name>
// (sni 仅受信证书场景输出;obfs 与 obfs-password 成对出现)。
func hysteria2ShareLink(n conf.Node) string {
	query := url.Values{}
	if n.Insecure {
		query.Set("insecure", "1")
	} else {
		query.Set("insecure", "0")
	}
	if n.ServerName != "" {
		query.Set("sni", n.ServerName)
	}
	if n.Obfs != "" {
		query.Set("obfs", "salamander")
		query.Set("obfs-password", n.Obfs)
	}
	// auth 是认证密码(userinfo),空格等特殊字符转义为 %20:QueryEscape
	// 把空格编码为 "+",而 url.Parse 的 userinfo 段不还原 "+"(仅 query
	// 段还原),密码含空格会导入失败
	auth := strings.ReplaceAll(url.QueryEscape(n.Password), "+", "%20")
	return fmt.Sprintf("hysteria2://%s@%s/?%s#%s", auth, shareHost(n),
		query.Encode(), url.PathEscape(shareFragment(n)))
}

// shareFragment 返回链接显示名(Name 空 → Address:Port 兜底)。
func shareFragment(n conf.Node) string {
	if n.Name != "" {
		return n.Name
	}
	return shareHost(n)
}

// shareHost 构造 host:port(IPv6 地址加方括号,域名/IPv4 原样)。
func shareHost(n conf.Node) string {
	addr := n.Address
	if strings.Contains(addr, ":") {
		addr = "[" + addr + "]"
	}
	return fmt.Sprintf("%s:%d", addr, n.Port)
}

// ===== 通用渲染辅助 =====

// base64StdEncode 是标准 base64 编码(带 padding,ss:// userinfo 用)。
func base64StdEncode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// renderFragment 渲染 JSON 片段(值已 jq 转义,占位符不带引号)。
//
// 调用说明:片段不是完整 JSON(不做语法校验),但保留原始缩进
// (TrimSpace 会吃掉片段首行缩进,片段用本函数,完整文档用 renderJSON)。
// 返回:渲染文本;占位符缺失时返回错误。
func renderFragment(tpl string, vals map[string]string) (string, error) {
	t, err := template.New("frag").Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("client.renderFragment: 解析模板: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vals); err != nil {
		return "", fmt.Errorf("client.renderFragment: 渲染: %w", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("<no value>")) {
		return "", fmt.Errorf("client.renderFragment: 模板占位符缺失")
	}
	return buf.String(), nil
}

// renderJSON 渲染完整 JSON 文档模板(值已 jq 转义,占位符不带引号)。
//
// 调用说明:模板为完整 JSON 文本;vals 的 string 值须为 JSON 字面量
// (jq 产物)。返回:渲染字节(不含末尾换行);语法校验失败或占位符
// 缺失时返回错误。
func renderJSON(tpl string, vals map[string]string) ([]byte, error) {
	t, err := template.New("json").Parse(tpl)
	if err != nil {
		return nil, fmt.Errorf("client.renderJSON: 解析模板: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vals); err != nil {
		return nil, fmt.Errorf("client.renderJSON: 渲染: %w", err)
	}
	out := buf.Bytes()
	if bytes.Contains(out, []byte("<no value>")) {
		return nil, fmt.Errorf("client.renderJSON: 模板占位符缺失")
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("client.renderJSON: 渲染结果不是合法 JSON")
	}
	return bytes.TrimSpace(out), nil
}

// jq 把字符串转义为 JSON 字面量(含引号;与 YAML 双引号字符串转义集
// 相同,clash 渲染复用)。
func jq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
