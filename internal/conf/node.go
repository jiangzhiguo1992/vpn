// Package conf 客户端节点模型:服务端配置反推客户端节点(部署闭环)。
//
// 设计决策:
//   - 客户端节点由服务端通道反推(Nodes),两端共享同一清单同一凭据,
//     服务端 config.json 与客户端任何产物之间不可能漂移
//   - 节点名 = <服务器名>-<协议短名>(vless/ss/h2),同时用于分享链接
//     fragment、clash 代理名、sing-box outbound tag
//   - Hysteria2 节点 insecure 由 server_name 推导:空(自签证书场景,
//     IP 直连或域名+自签)= true;非空(受信证书域名场景)= false,
//     渲染方直接取用,无需业务判断
//   - 端口直接取回填后的实际值(ListenPort 兜底,防未调 Backfill 的调用方)
//
// 职责边界:字段映射与推导,零文件 I/O、零网络操作;
// 校验在 validate.go,渲染在 internal/server 与 internal/client。
//
// 使用示例:
//
//	nodes := srv.Nodes() // []Node,贴进客户端渲染器
package conf

// NodeType 是节点协议类型(客户端渲染按类型分支)。
type NodeType string

// 支持的节点协议类型(与清单通道一一对应)。
const (
	TypeVLESSReality NodeType = "vless"       // VLESS+Reality(TCP,主力抗封锁)
	TypeShadowsocks  NodeType = "shadowsocks" // Shadowsocks(TCP+UDP,保底兼容)
	TypeHysteria2    NodeType = "hysteria2"   // Hysteria2(UDP/QUIC,逃生提速)
)

// ProtocolSuffix 返回节点名协议短名(vless/ss/h2,节点名与 tag 用)。
func (t NodeType) ProtocolSuffix() string {
	switch t {
	case TypeVLESSReality:
		return "vless"
	case TypeShadowsocks:
		return "ss"
	case TypeHysteria2:
		return "h2"
	}
	return string(t)
}

// Node 是单节点(一台服务器的一个通道)的客户端侧视图。
type Node struct {
	Name       string   // 节点名:<服务器名>-<协议短名>(客户端显示名与 tag)
	Location   string   // 位置/地区(客户端显示用,可空)
	Type       NodeType // 协议类型
	Address    string   // 服务器地址(域名或裸 IP,IPv6 不带括号,渲染时处理)
	Port       uint16   // 实际端口(回填后)
	UUID       string   // VLESS:用户 ID
	ServerName string   // VLESS:伪装站点 / H2:证书 SNI
	PublicKey  string   // VLESS:Reality 公钥(私钥推导)
	ShortID    string   // VLESS:short id
	Method     string   // SS:加密方式
	Password   string   // SS/H2:认证密码
	Obfs       string   // H2:salamander 混淆密码(空=不启用)
	Insecure   bool     // H2:跳过证书校验(自签场景,由 server_name 推导)
}

// Nodes 反推该服务器的全部客户端节点(每通道一个,顺序 vless/ss/h2)。
//
// 调用说明:Backfill 后调用(端口与凭据已就位);VLESS 通道的 Reality
// 公钥由私钥推导。返回:客户端节点列表(至少一个,空通道服务器由
// Validate 拦截)。
func (s *Server) Nodes() []Node {
	out := make([]Node, 0, 3)
	if s.VLESS != nil {
		v := s.VLESS
		out = append(out, Node{
			Name:       s.Name + "-vless",
			Location:   s.Location,
			Type:       TypeVLESSReality,
			Address:    s.Address,
			Port:       v.ListenPort(),
			UUID:       v.UUID,
			ServerName: v.ServerName,
			PublicKey:  PublicKeyFromPrivateKey(v.PrivateKey),
			ShortID:    v.ShortID,
		})
	}
	if s.Shadowsocks != nil {
		ss := s.Shadowsocks
		out = append(out, Node{
			Name:     s.Name + "-ss",
			Location: s.Location,
			Type:     TypeShadowsocks,
			Address:  s.Address,
			Port:     ss.ListenPort(),
			Method:   ss.MethodName(),
			Password: ss.Password,
		})
	}
	if s.Hysteria2 != nil {
		h := s.Hysteria2
		out = append(out, Node{
			Name:       s.Name + "-h2",
			Location:   s.Location,
			Type:       TypeHysteria2,
			Address:    s.Address,
			Port:       h.ListenPort(),
			ServerName: h.ServerName,
			Password:   h.Password,
			Obfs:       h.ObfsPassword,
			// 空 server_name 意味着 IP+自签证书(cert.sh 生成),客户端必须
			// 跳过证书校验;填了 server_name 则是受信证书域名,正常校验
			Insecure: h.ServerName == "",
		})
	}
	return out
}
