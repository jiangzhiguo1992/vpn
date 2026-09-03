// Package conf 清单校验:字段合法性 + 跨字段一致性。
//
// 设计决策:
//   - 校验在回填之前(validate 只接受"用户填写形态"):用户不填的端口 0、
//     空 method、空凭据都合法,由 Backfill 补齐;校验拒绝的是"填了但填错"
//     与"必须填而没填"(如 server_name)
//   - name 同时是产物目录名与客户端 tag/链接 fragment,严格限定字符集
//     防路径穿越与配置注入
//   - address 是客户端连接地址,拒绝 host:port 误填(会污染链接与配置),
//     拒绝 [] 包裹与空白;IPv6 裸地址(含冒号)必须能通过 netip.ParseAddr
//   - 端口冲突校验按"回填后口径"比较(0 视为默认端口),与服务端渲染一致
//
// 职责边界:纯校验返回 error,不修改模型(修改见 io.go 的 Backfill)。
package conf

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// UUIDPattern 是标准 UUID 形态(8-4-4-4-12 十六进制)。
var UUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// namePattern 是服务器名合法字符集(ASCII 字母数字 + . _ -)。
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Validate 校验整个清单(空清单、重名、每台服务器逐项)。
//
// 调用说明:在 Load 后、Backfill 前调用;不修改任何字段。
// 返回:任一服务器不合法时返回含服务器名的包装错误。
func (inv *Inventory) Validate() error {
	if len(inv.Servers) == 0 {
		return fmt.Errorf("conf.Validate: servers 为空,请至少填写一台服务器")
	}
	seen := make(map[string]bool, len(inv.Servers))
	for _, s := range inv.Servers {
		if s == nil {
			return fmt.Errorf("conf.Validate: 存在空服务器条目")
		}
		if seen[s.Name] {
			return fmt.Errorf("conf.Validate: 服务器名 %q 重复", s.Name)
		}
		seen[s.Name] = true
		if err := s.Validate(); err != nil {
			return fmt.Errorf("conf.Validate: 服务器 %q: %w", s.Name, err)
		}
	}
	return nil
}

// Validate 校验单台服务器(身份字段 + 通道内字段 + 跨通道端口冲突)。
//
// 调用说明:校验对象是"用户填写形态"(端口可为 0,凭据可为空)。
// 返回:任一字段不合法时返回包装错误(含字段名与原因)。
func (s *Server) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	if !namePattern.MatchString(s.Name) || s.Name == "." || s.Name == ".." {
		return fmt.Errorf("name %q 非法:仅允许字母/数字/._- 组合(ASCII),且不能是 . 或 ..", s.Name)
	}
	if err := ValidAddress(s.Address); err != nil {
		return fmt.Errorf("address 非法: %w", err)
	}
	if !s.HasChannel() {
		return fmt.Errorf("vless/shadowsocks/hysteria2 至少配置一个通道")
	}
	if s.SSH != nil && (s.SSH.Port < 0 || s.SSH.Port > 65535) {
		return fmt.Errorf("ssh.port %d 超出范围(1-65535)", s.SSH.Port)
	}
	if s.VLESS != nil {
		if err := validateVLESS(s.VLESS); err != nil {
			return fmt.Errorf("vless: %w", err)
		}
	}
	if s.Shadowsocks != nil {
		if err := validateSS(s.Shadowsocks); err != nil {
			return fmt.Errorf("shadowsocks: %w", err)
		}
	}
	if s.Hysteria2 != nil {
		if err := validateH2(s.Hysteria2); err != nil {
			return fmt.Errorf("hysteria2: %w", err)
		}
	}
	return s.validatePortConflict()
}

// validateVLESS 校验 VLESS 通道(伪装站点必填且格式合法,已有凭据必须合规)。
func validateVLESS(v *VLESSConfig) error {
	if v.ServerName == "" {
		return fmt.Errorf("server_name(伪装站点)必填,如 www.apple.com")
	}
	// 拒绝含空白(误粘贴空格)或含 "://"(误填完整 URL)的值;IPv6 冒号与
	// 端口形态(example.com:8443)不做拦截(容忍,不误伤)
	if strings.ContainsAny(v.ServerName, " \t") || strings.Contains(v.ServerName, "://") {
		return fmt.Errorf("server_name(伪装站点)%q 非法:不能含空白或 URL scheme,请填域名或 IP", v.ServerName)
	}
	if v.PrivateKey != "" {
		if _, err := decodePrivateKey(v.PrivateKey); err != nil {
			return fmt.Errorf("private_key 不是合法的 base64url 32 字节密钥: %w", err)
		}
	}
	if v.UUID != "" && !UUIDPattern.MatchString(v.UUID) {
		return fmt.Errorf("uuid %q 非法:需标准 UUID 形态 8-4-4-4-12 十六进制", truncateSecret(v.UUID))
	}
	if v.ShortID != "" {
		// 偶数长度 2-16 字符(=1-8 字节),与 sing-box Reality 接受范围一致
		if len(v.ShortID) < 2 || len(v.ShortID) > 16 || len(v.ShortID)%2 != 0 {
			return fmt.Errorf("short_id %q 非法:需偶数位 hex,2-16 字符(=1-8 字节)", v.ShortID)
		}
		if _, err := hex.DecodeString(v.ShortID); err != nil {
			return fmt.Errorf("short_id %q 不是合法 hex: %w", v.ShortID, err)
		}
	}
	return nil
}

// validateSS 校验 Shadowsocks 通道(加密方式必须在白名单内)。
func validateSS(s *SSConfig) error {
	if s.Method != "" && !slices.Contains(SSAllowedMethods, s.Method) {
		return fmt.Errorf("method %q 不在支持列表内(可选 %s,空=默认 aes-256-gcm)", s.Method, strings.Join(SSAllowedMethods, "/"))
	}
	return nil
}

// validateH2 校验 Hysteria2 通道(凭据非空时格式合理即可,obfs 密码任意串)。
func validateH2(h *H2Config) error {
	if h.ServerName != "" && (strings.ContainsAny(h.ServerName, " \t") || strings.Contains(h.ServerName, "://")) {
		return fmt.Errorf("server_name(证书域名)%q 非法:不能含空白或 URL scheme", h.ServerName)
	}
	return nil
}

// validatePortConflict 校验同服务器内各通道实际监听端口互不冲突(0 视为默认)。
func (s *Server) validatePortConflict() error {
	ports := map[uint16]string{}
	add := func(channel string, p uint16) {
		if _, dup := ports[p]; dup {
			return // 冲突已记录,不覆盖首报通道
		}
		ports[p] = channel
	}
	if s.VLESS != nil {
		add("vless", s.VLESS.ListenPort())
	}
	if s.Shadowsocks != nil {
		add("shadowsocks", s.Shadowsocks.ListenPort())
	}
	if s.Hysteria2 != nil {
		add("hysteria2", s.Hysteria2.ListenPort())
	}
	if len(ports) != s.channelCount() {
		return fmt.Errorf("通道监听端口冲突:各通道须使用不同端口(默认 vless 443 / shadowsocks 8388 / hysteria2 8443)")
	}
	return nil
}

// channelCount 返回已配置通道数(validatePortConflict 的冲突判定辅助)。
func (s *Server) channelCount() int {
	n := 0
	if s.VLESS != nil {
		n++
	}
	if s.Shadowsocks != nil {
		n++
	}
	if s.Hysteria2 != nil {
		n++
	}
	return n
}

// ValidAddress 校验客户端连接地址(域名或裸 IP)。
//
// 调用说明:地址会进入分享链接/客户端配置/SSH 目标,格式缺口两端同源,
// 此处统一拦截。接受:域名、IPv4、IPv6 裸地址;拒绝:host:port 误填、
// [] 包裹、空白、URL scheme、路径分隔符。
func ValidAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("不能为空")
	}
	if strings.ContainsAny(addr, " \t") || strings.Contains(addr, "://") || strings.Contains(addr, "[") || strings.Contains(addr, "]") || strings.ContainsAny(addr, "/?#") {
		return fmt.Errorf("%q 非法:请填域名或裸 IP(不含端口/协议前缀/括号/路径)", addr)
	}
	if strings.Contains(addr, ":") { // IPv6 候选:必须能解析为合法地址
		if _, err := netip.ParseAddr(addr); err != nil {
			return fmt.Errorf("%q 不是合法 IPv6 地址(误填了 host:port?)", addr)
		}
		return nil
	}
	// 无冒号:IPv4 或域名
	if ip := net.ParseIP(addr); ip != nil {
		return nil
	}
	if !hostnamePattern.MatchString(addr) {
		return fmt.Errorf("%q 不是合法域名或 IP", addr)
	}
	return nil
}

// hostnamePattern 是宽松域名形态(字母数字开头结尾,中段字母数字/连字符,
// 可含多级标签)。单标签(如 vps)也放行,交由 DNS 解析兜底。
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// truncateSecret 截断凭据回显(前 8 字符 + ...):凭据是认证依据,完整
// 回显会泄入错误信息/日志,截断保留可辨识前缀。
func truncateSecret(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8] + "..."
}
