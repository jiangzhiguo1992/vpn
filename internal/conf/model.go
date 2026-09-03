// Package conf 服务器清单模型:JSON 结构与默认值。
//
// 用途:清单(servers.json)是全部产物的单一事实来源,用户在清单里填写
// 服务器信息(身份字段),Agent 自动生成并回填凭据字段,服务端产物与
// 客户端产物都从同一份清单渲染,保证两端凭据一致(防漂移)。
//
// 设计决策:
//   - 每台服务器最多三通道(VLESS+Reality / Shadowsocks / Hysteria2),
//     至少一个;协议选择由客户端生态兼容矩阵决定(见 docs/clients.md)
//   - 字段分两类:身份字段(用户填写,地址/端口/伪装站点/加密方式)与
//     凭据字段(回填生成,私钥/UUID/ShortID/密码)。凭据生成细节见
//     keys.go,回填时机见 conf.Inventory.Backfill
//   - 端口 0 表示"使用默认端口"(443/8388/8443),Backfill 时回填为
//     默认值,后续渲染直接取值,无两套口径
//   - Shadowsocks 仅支持经典 method(兼容面最广,Clash/sing-box/移动端
//     app 全支持);SS2022 系需要 base64 PSK 且部分客户端不支持,不纳入
//
// 职责边界:纯数据模型与默认值,零文件 I/O(见 io.go)、零随机数
// (见 keys.go)、零校验(见 validate.go)。
//
// 使用示例:
//
//	inv := &conf.Inventory{}
//	if err := conf.LoadFile("servers.json", inv); err != nil { ... }
//	if err := inv.Validate(); err != nil { ... }
//	if err := inv.Backfill(); err != nil { ... } // 凭据回填,幂等
package conf

// 各通道默认监听端口(用户不填或填 0 时回填)。
const (
	DefaultVLESSListenPort       uint16 = 443  // VLESS+Reality 默认监听端口
	DefaultShadowsocksListenPort uint16 = 8388 // Shadowsocks 默认监听端口
	DefaultHysteria2ListenPort   uint16 = 8443 // Hysteria2 默认监听端口
)

// 默认 Shadowsocks 加密方式(经典 SS,硬件加速与兼容面平衡)。
const DefaultShadowsocksMethod = "aes-256-gcm"

// SSAllowedMethods 是 Shadowsocks 允许的加密方式白名单(经典 SS 系,
// sing-box/clash 内核与主流移动端 app 均支持)。
var SSAllowedMethods = []string{
	"aes-256-gcm",
	"aes-128-gcm",
	"chacha20-ietf-poly1305",
	"xchacha20-ietf-poly1305",
}

// Inventory 是整个清单文件(servers.json)的根结构。
type Inventory struct {
	Servers []*Server `json:"servers"`
}

// Server 是单台服务器的全部信息(身份 + 三通道)。
type Server struct {
	Name        string       `json:"name"`                  // 服务器唯一标识,产物目录名与客户端节点名前缀
	Location    string       `json:"location,omitempty"`    // 位置/地区(如"香港"),透传到客户端节点显示名
	Address     string       `json:"address"`               // 客户端连接地址:域名或裸 IP(IPv6 直接填)
	SSH         *SSHConfig   `json:"ssh,omitempty"`         // SSH 部署参数(仅 deploy 用)
	VLESS       *VLESSConfig `json:"vless,omitempty"`       // VLESS+Reality 通道(可选)
	Shadowsocks *SSConfig    `json:"shadowsocks,omitempty"` // Shadowsocks 通道(可选)
	Hysteria2   *H2Config    `json:"hysteria2,omitempty"`   // Hysteria2 通道(可选)
}

// SSHConfig 是 SSH 部署参数(仅 deploy 子命令使用,gen 忽略)。
type SSHConfig struct {
	User string `json:"user,omitempty"` // 部署用户名,空默认 root
	Port int    `json:"port,omitempty"` // SSH 端口,0 或 22 视为默认 22
}

// VLESSConfig 是 VLESS+Reality 通道参数(主通道,伪装 TLS 流量)。
type VLESSConfig struct {
	Port       uint16 `json:"port,omitempty"`        // 监听端口,0 默认 443
	ServerName string `json:"server_name"`           // 伪装站点(Reality 握手目标),必填,如 www.apple.com
	PrivateKey string `json:"private_key,omitempty"` // Reality 私钥(base64url x25519),回填生成
	UUID       string `json:"uuid,omitempty"`        // 用户 ID(标准 UUID v4),回填生成
	ShortID    string `json:"short_id,omitempty"`    // Reality short id(8 字节 hex),回填生成
}

// SSConfig 是 Shadowsocks 通道参数(保底兼容通道)。
type SSConfig struct {
	Port     uint16 `json:"port,omitempty"`     // 监听端口,0 默认 8388(TCP+UDP)
	Method   string `json:"method,omitempty"`   // 加密方式,空默认 aes-256-gcm(见 SSAllowedMethods)
	Password string `json:"password,omitempty"` // 认证密码,回填生成
}

// H2Config 是 Hysteria2 通道参数(UDP/QUIC 逃生通道)。
type H2Config struct {
	Port         uint16 `json:"port,omitempty"`          // 监听端口,0 默认 8443(仅 UDP)
	Password     string `json:"password,omitempty"`      // 认证密码,回填生成
	ServerName   string `json:"server_name,omitempty"`   // 证书域名:空=IP+自签证书(客户端跳过校验);填=受信证书域名(客户端校验证书)
	ObfsPassword string `json:"obfs_password,omitempty"` // salamander 混淆密码,空=不启用混淆
}

// ListenPort 返回通道实际监听端口(0 已回填,此处兜底防御未调 Backfill 的调用方)。
func (v *VLESSConfig) ListenPort() uint16 {
	if v == nil || v.Port == 0 {
		return DefaultVLESSListenPort
	}
	return v.Port
}

// ListenPort 返回通道实际监听端口(0 已回填,此处兜底防御未调 Backfill 的调用方)。
func (s *SSConfig) ListenPort() uint16 {
	if s == nil || s.Port == 0 {
		return DefaultShadowsocksListenPort
	}
	return s.Port
}

// ListenPort 返回通道实际监听端口(0 已回填,此处兜底防御未调 Backfill 的调用方)。
func (h *H2Config) ListenPort() uint16 {
	if h == nil || h.Port == 0 {
		return DefaultHysteria2ListenPort
	}
	return h.Port
}

// Method 返回实际加密方式(空已回填默认,此处兜底防御)。
func (s *SSConfig) MethodName() string {
	if s == nil || s.Method == "" {
		return DefaultShadowsocksMethod
	}
	return s.Method
}

// HasChannel 返回该服务器是否配置了至少一个协议通道。
func (s *Server) HasChannel() bool {
	return s.VLESS != nil || s.Shadowsocks != nil || s.Hysteria2 != nil
}
