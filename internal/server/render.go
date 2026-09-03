// Package server 服务端产物生成:sing-box config.json + 编排与部署脚本。
//
// 用途:由清单单台服务器(conf.Server)渲染服务端全部产物,产物在
// <out>/servers/<name>/ 下,scp 到服务器即可一键部署。
//
// 设计决策:
//   - 服务端 = 官方 sing-box 镜像容器(network_mode: host 直出端口,
//     免端口映射),镜像版本锁 tag(常量 ImageVersion,与 docs 同步点)
//   - config.json 用手写 JSON 模板渲染而非依赖 sing-box option 库:
//     零第三方依赖、构建秒级、产物可读;字符串值统一经 json.Marshal
//     转义为 JSON 字面量再贴模板(防密码/SNI 含引号注入),模板插槽
//     不带引号
//   - 每通道一个独立模板片段(保序),按 vless/ss/h2 顺序 join;渲染后
//     整体 json.Valid 双保险(text/template 对 map 缺 key 静默输出
//     <no value>,语法校验兜底防残缺配置)
//   - Shadowsocks inbound 不写 network 字段:sing-box 留空默认同时监听
//     tcp/udp(UDP 中继,游戏/通话场景需要),显式 "tcp,udp" 反而被校验
//     拒绝;Hysteria2 是纯 QUIC 仅 UDP,防火墙放行口径见 deploy.sh
//   - 服务端流量直出:outbounds 恒 direct + route.final=direct(无代理
//     出站);Hysteria2 服务端 TLS 用证书路径模式(单证书,无需 server_name)
//
// 职责边界:纯渲染与产物写盘(artifact.go);清单模型/校验/回填在
// internal/conf;产物上传与远程执行在 internal/deploy。
//
// 使用示例:
//
//	data, err := server.RenderConfig(srv) // config.json 内容
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"vpn/internal/conf"
)

// ImageVersion 是 sing-box 官方镜像锁版本(与 docs/deployment.md 的版本
// 说明同源,升级时两处必须一致)。
const ImageVersion = "v1.14.0"

// ImageRef 是 compose/脚本引用的镜像(ghcr 官方源)。
const ImageRef = "ghcr.io/sagernet/sing-box:" + ImageVersion

// ImageMirror 是 ghcr 不可达时的备源(docker.io 官方镜像同源发布)。
const ImageMirror = "docker.io/sagernet/sing-box:" + ImageVersion

// RenderConfig 渲染单台服务器的 sing-box 服务端配置(config.json 内容)。
//
// 调用说明:conf.Server 须已通过 Validate + Backfill(端口与凭据已就位);
// 凭据缺失时此处报错(避免产出残缺配置)。参数 s:单台服务器清单条目。
// 返回:缩进 JSON 字节(不含末尾换行);渲染不合法或字段缺失时返回错误。
func RenderConfig(s *conf.Server) ([]byte, error) {
	if !s.HasChannel() {
		return nil, fmt.Errorf("server.RenderConfig: 服务器 %q 无协议通道", s.Name)
	}
	vals := map[string]string{}
	frags := make([]string, 0, 3)
	if s.VLESS != nil {
		v := s.VLESS
		if v.UUID == "" || v.PrivateKey == "" || v.ShortID == "" {
			return nil, fmt.Errorf("server.RenderConfig: 服务器 %q vless 凭据未回填(先执行清单回填再生成)", s.Name)
		}
		if v.ServerName == "" {
			return nil, fmt.Errorf("server.RenderConfig: 服务器 %q vless server_name(伪装站点)为空", s.Name)
		}
		vals["VLESS_UUID"] = jq(v.UUID)
		vals["VLESS_SERVER_NAME"] = jq(v.ServerName)
		vals["VLESS_PRIVATE_KEY"] = jq(v.PrivateKey)
		vals["VLESS_SHORT_ID"] = jq(v.ShortID)
		vals["VLESS_PORT"] = fmt.Sprint(v.ListenPort())
		frag, err := render(vlessFragment, vals)
		if err != nil {
			return nil, fmt.Errorf("server.RenderConfig: vless 片段: %w", err)
		}
		frags = append(frags, frag)
	}
	if s.Shadowsocks != nil {
		ss := s.Shadowsocks
		if ss.Password == "" {
			return nil, fmt.Errorf("server.RenderConfig: 服务器 %q shadowsocks 密码未回填", s.Name)
		}
		vals["SS_METHOD"] = jq(ss.MethodName())
		vals["SS_PASSWORD"] = jq(ss.Password)
		vals["SS_PORT"] = fmt.Sprint(ss.ListenPort())
		frag, err := render(ssFragment, vals)
		if err != nil {
			return nil, fmt.Errorf("server.RenderConfig: shadowsocks 片段: %w", err)
		}
		frags = append(frags, frag)
	}
	if s.Hysteria2 != nil {
		h := s.Hysteria2
		if h.Password == "" {
			return nil, fmt.Errorf("server.RenderConfig: 服务器 %q hysteria2 密码未回填", s.Name)
		}
		vals["H2_PASSWORD"] = jq(h.Password)
		vals["H2_PORT"] = fmt.Sprint(h.ListenPort())
		obfs := ""
		if h.ObfsPassword != "" {
			vals["H2_OBFS_PASSWORD"] = jq(h.ObfsPassword)
			obfs = h2ObfsFragment
		}
		frag, err := render(h2FragmentPrefix+obfs+h2FragmentSuffix, vals)
		if err != nil {
			return nil, fmt.Errorf("server.RenderConfig: hysteria2 片段: %w", err)
		}
		frags = append(frags, frag)
	}
	var buf bytes.Buffer
	buf.WriteString(configHeader)
	buf.WriteString(strings.Join(frags, ",\n"))
	buf.WriteString("\n")
	buf.WriteString(configFooter)
	out := buf.Bytes()
	// 双保险:text/template 对 map 缺 key 输出 <no value>,静默产出坏 JSON;
	// 渲染后统一语法校验,不合法立即报错而非产出残缺配置
	if !json.Valid(out) {
		return nil, fmt.Errorf("server.RenderConfig: 渲染结果不是合法 JSON(模板占位符缺失?),请检查清单字段")
	}
	return bytes.TrimSpace(out), nil
}

// render 用 vals 渲染 tpl 并校验片段内占位符齐全(text/template 对 map
// 缺 key 输出 <no value>,渲染前先探明缺失,给可读错误)。
func render(tpl string, vals map[string]string) (string, error) {
	t, err := template.New("frag").Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("解析模板: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vals); err != nil {
		return "", fmt.Errorf("渲染: %w", err)
	}
	out := buf.String()
	if strings.Contains(out, "<no value>") {
		return "", fmt.Errorf("模板占位符缺失(清单字段未回填?)")
	}
	return out, nil
}

// jq 把字符串转义为 JSON 字面量(含引号),供模板插槽直接引用。
func jq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ===== 模板片段 =====

const configHeader = `{
  "log": {
    "level": "info"
  },
  "inbounds": [
`

const configFooter = `  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "final": "direct"
  }
}
`

// vlessFragment 是 VLESS+Reality inbound(TCP,伪装 TLS;Reality 握手
// server 恒 443,与监听端口无关)。
const vlessFragment = `    {
      "type": "vless",
      "tag": "vless-in",
      "listen": "::",
      "listen_port": {{.VLESS_PORT}},
      "users": [
        {
          "name": "default",
          "uuid": {{.VLESS_UUID}},
          "flow": "xtls-rprx-vision"
        }
      ],
      "tls": {
        "enabled": true,
        "server_name": {{.VLESS_SERVER_NAME}},
        "reality": {
          "enabled": true,
          "handshake": {
            "server": {{.VLESS_SERVER_NAME}},
            "server_port": 443
          },
          "private_key": {{.VLESS_PRIVATE_KEY}},
          "short_id": [{{.VLESS_SHORT_ID}}]
        }
      }
    }`

// ssFragment 是 Shadowsocks inbound(TCP+UDP 双栈:sing-box 的 network
// 字段留空时默认同时监听 tcp/udp,故不写 network;显式 "tcp,udp" 会被
// 校验拒绝,见 sing-box option.NetworkList)。
const ssFragment = `    {
      "type": "shadowsocks",
      "tag": "ss-in",
      "listen": "::",
      "listen_port": {{.SS_PORT}},
      "method": {{.SS_METHOD}},
      "password": {{.SS_PASSWORD}}
    }`

// h2FragmentPrefix 与 h2FragmentSuffix 是 Hysteria2 inbound 的前后半段
// (中间按需插入 h2ObfsFragment,保持 obfs 字段在 tls 之后的可读布局)。
const h2FragmentPrefix = `    {
      "type": "hysteria2",
      "tag": "hy2-in",
      "listen": "::",
      "listen_port": {{.H2_PORT}},
      "users": [
        {
          "name": "default",
          "password": {{.H2_PASSWORD}}
        }
      ],
      "tls": {
        "enabled": true,
        "certificate_path": "/etc/sing/cert.pem",
        "key_path": "/etc/sing/key.pem"
      }
`

// h2ObfsFragment 是 Hysteria2 salamander 混淆段(用户配置了 obfs_password
// 时插入)。
const h2ObfsFragment = `,
      "obfs": {
        "type": "salamander",
        "password": {{.H2_OBFS_PASSWORD}}
      }
`

const h2FragmentSuffix = `    }`
