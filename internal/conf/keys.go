// Package conf 凭据生成:UUID / hex / Reality 密钥对。
//
// 用途:全部随机凭据用 crypto/rand 手写生成,零第三方库依赖
// (x25519 用标准库 crypto/ecdh,Reality 协议与其完全兼容)。
//
// 设计决策:
//   - Reality 公/私钥用 base64.RawURLEncoding(无 padding,Reality 协议
//     格式);公钥由私钥推导(客户端只需要公钥)
//   - 凭据全部在本机生成并回填清单,服务器不生成任何密钥,只消费
//     config.json,由此"服务器无状态":删机重建不影响客户端
//
// 职责边界:纯随机数生成,零文件 I/O、零网络操作。
package conf

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// RealityKeys 是 Reality x25519 密钥对。
type RealityKeys struct {
	PrivateKey string // 服务端用(配置 vless.private_key)
	PublicKey  string // 客户端用(生成 vless:// 链接与 sing-box 配置)
}

// GenerateRealityKeys 生成 Reality x25519 密钥对。
//
// 返回:密钥对(base64.RawURLEncoding 无 padding,各 43 字符);
// 随机源失败时返回包装错误。
func GenerateRealityKeys() (*RealityKeys, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("conf.GenerateRealityKeys: %w", err)
	}
	return &RealityKeys{
		PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
	}, nil
}

// PublicKeyFromPrivateKey 从 Reality 私钥推导公钥;私钥非法时返回空串
// (调用方如 conf.Server.Nodes 无 error 通道,校验由 validate 前置拦截)。
func PublicKeyFromPrivateKey(privateKey string) string {
	raw, err := decodePrivateKey(privateKey)
	if err != nil {
		return ""
	}
	priv, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
}

// decodePrivateKey 解码 Reality 私钥:base64url(无 padding)+ 32 字节长度校验。
func decodePrivateKey(privateKey string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("conf.decodePrivateKey: base64url 解码失败: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("conf.decodePrivateKey: 解码后长度 %d 字节, want 32 字节", len(raw))
	}
	return raw, nil
}

// NewUUID 生成标准 UUID v4(crypto/rand 16 字节 + 版本/变体位)。
func NewUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("conf.NewUUID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40 // 版本 4
	b[8] = b[8]&0x3f | 0x80 // 变体 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// NewHex 生成 n 字节随机 hex(2n 字符);n<=0 时报错(rand.Read 对空切片
// 返回成功会静默产出空串)。
func NewHex(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("conf.NewHex: 长度必须为正数, got %d", n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("conf.NewHex: %w", err)
	}
	return hex.EncodeToString(b), nil
}
