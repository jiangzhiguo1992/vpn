// Package conf 清单 IO:加载 / 凭据回填 / 原子保存。
//
// 设计决策:
//   - 保存用临时文件 + rename 原子替换(写中途崩溃不损坏原清单),
//     显式 Chmod 0600(WriteFile 的 mode 仅创建时生效,已存在文件继承
//     旧权限,重跑回填会残留 0644)
//   - 加载用 DisallowUnknownFields 严格模式:用户把字段名拼错(如
//     private_key 拼成 privatekey)会静默丢字段,随后凭据被重新生成,
//     已分发客户端全部失效,严格模式把错误提前暴露
//   - Backfill 幂等:非空字段不重新生成;中途失败时已回填部分保留,
//     重跑不会产生新随机数(已分发客户端不受影响)
//   - 凭据全部在本机生成并持久化到清单,服务器不生成任何密钥:
//     "服务器无状态",删机重建不改变客户端
//
// 职责边界:文件读写 + 字段回填;字段校验在 validate.go。
//
// 使用示例:
//
//	inv := &conf.Inventory{}
//	if err := conf.LoadFile("servers.json", inv); err != nil { ... }
//	if err := inv.Backfill(); err != nil { ... }
//	if err := conf.SaveFile("servers.json", inv); err != nil { ... }
package conf

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadFile 从 path 读取并解析清单(严格模式,未知字段报错)。
//
// 参数 inv:解析目标(调用方预先创建);返回:IO 或解析错误(含路径)。
func LoadFile(path string, inv *Inventory) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("conf.LoadFile: 打开 %s: %w", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(inv); err != nil {
		return fmt.Errorf("conf.LoadFile: 解析 %s: %w(请对照 example-servers.json 检查字段名)", path, err)
	}
	return nil
}

// SaveFile 以 0600 权限原子保存清单(临时文件 + rename)。
//
// 参数 path:目标路径;inv:待保存清单。
// 返回:写临时文件/rename/Chmod 任一失败时返回包装错误。
func SaveFile(path string, inv *Inventory) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return fmt.Errorf("conf.SaveFile: 序列化: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("conf.SaveFile: 写临时文件 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("conf.SaveFile: 原子替换 %s: %w", path, err)
	}
	// rename 后目标可能继承旧权限(历史 0644),显式收紧
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("conf.SaveFile: 设置 %s 权限 0600: %w", path, err)
	}
	return nil
}

// Backfill 回填全部默认值与随机凭据(幂等:非空字段不覆盖)。
//
// 调用说明:Validate 通过后调用;全部回填完成才返回 nil,中途失败时
// 已回填部分保留在 inv 中(重跑复用,不产生新随机数)。
// 回填范围:端口 0 到默认端口;vless 的 Reality 私钥/UUID/short_id;
// shadowsocks 的 method/密码;hysteria2 的密码。
// 返回:任一随机生成失败时返回含服务器名的包装错误。
func (inv *Inventory) Backfill() error {
	for _, s := range inv.Servers {
		if s == nil {
			continue // Validate 已拦截,此处防御
		}
		if err := s.backfill(); err != nil {
			return fmt.Errorf("conf.Backfill: 服务器 %q: %w", s.Name, err)
		}
	}
	return nil
}

// backfill 回填单台服务器(端口默认值 + 各通道凭据)。
func (s *Server) backfill() error {
	if s.VLESS != nil {
		v := s.VLESS
		if v.Port == 0 {
			v.Port = DefaultVLESSListenPort
		}
		if v.PrivateKey == "" {
			keys, err := GenerateRealityKeys()
			if err != nil {
				return fmt.Errorf("生成 Reality 密钥对: %w", err)
			}
			v.PrivateKey = keys.PrivateKey
		}
		if v.UUID == "" {
			uuid, err := NewUUID()
			if err != nil {
				return fmt.Errorf("生成 uuid: %w", err)
			}
			v.UUID = uuid
		}
		if v.ShortID == "" {
			sid, err := NewHex(8)
			if err != nil {
				return fmt.Errorf("生成 short_id: %w", err)
			}
			v.ShortID = sid
		}
	}
	if s.Shadowsocks != nil {
		ss := s.Shadowsocks
		if ss.Port == 0 {
			ss.Port = DefaultShadowsocksListenPort
		}
		if ss.Method == "" {
			ss.Method = DefaultShadowsocksMethod
		}
		if ss.Password == "" {
			pw, err := NewHex(32)
			if err != nil {
				return fmt.Errorf("生成 shadowsocks 密码: %w", err)
			}
			ss.Password = pw
		}
	}
	if s.Hysteria2 != nil {
		h := s.Hysteria2
		if h.Port == 0 {
			h.Port = DefaultHysteria2ListenPort
		}
		if h.Password == "" {
			pw, err := NewHex(32)
			if err != nil {
				return fmt.Errorf("生成 hysteria2 密码: %w", err)
			}
			h.Password = pw
		}
	}
	return nil
}
