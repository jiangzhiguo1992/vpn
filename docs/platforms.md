# 平台兼容矩阵

服务端三通道协议(Reality / SS / Hy2)与主流客户端在各平台的组合矩阵。
"覆盖全部主流场景:Windows / macOS / Linux / Android / iOS / 网关盒子"的落地对照。

## 平台 × 客户端总表

| 平台 | 推荐客户端 | 使用的分发物 | 主要通道 | 备选 |
|---|---|---|---|---|
| Windows | Clash Verge Rev | clash.yaml | vless(Reality) | Hiddify / sing-box CLI |
| macOS | Clash Verge Rev | clash.yaml | vless(Reality) | Hiddify / sing-box(SFI) |
| Linux 桌面 | Clash Verge Rev / mihomo | clash.yaml | vless(Reality) | sing-box CLI |
| Android | Hiddify | links.txt(链接/扫码) | vless(Reality) | sing-box(SFA) / Clash Meta for Android |
| iOS/iPadOS | Hiddify | links.txt(链接/扫码) | vless(Reality) | sing-box(SFI) / Shadowrocket 等 |
| 网关盒子(OpenWrt 软路由) | OpenClash(mihomo 内核) | clash.yaml(上传/订阅) | vless(Reality) | 裸 mihomo / sing-box |
| 其他(电视盒子 Android TV 等) | Hiddify 或对应 Android 客户端 | links.txt | vless(Reality) | ss 保底 |

> 详细分客户端导入步骤见 [docs/clients.md](clients.md) 与 clients/ 下各文档。

## 协议在各客户端的能力(决定了上面的选型)

| 协议/传输 | sing-box 官方 | Hiddify | Clash Verge Rev(mihomo) | iOS 第三方 app | 说明 |
|---|---|---|---|---|---|
| VLESS+Reality(TCP 443) | 支持 | 支持 | 支持(内核需 2024+ 版本) | 部分支持(Shadowrocket 等新版支持) | 主力通道,抗封锁 |
| Shadowsocks(TCP+UDP) | 支持 | 支持 | 支持 | 支持(最老牌格式) | 保底通道,任何客户端兜底 |
| Hysteria2(UDP 8443) | 支持 | 支持 | 支持(内核需 v1.18+) | 部分支持 | QUIC 逃生通道 |

**iOS 建议**:若使用的第三方 app 不支持 Reality/Hy2,选 ss 节点(兼容最广)。
**网关盒子建议**:OpenClash 内核对三通道支持完整,直接全量导入。

## 每台服务器节点形态(多服务器时自动聚合)

| 节点名 | 协议 | 端口 | 备注 |
|---|---|---|---|
| `<服务器名>-vless` | VLESS+Reality | 443 TCP | flow=xtls-rprx-vision,fp=chrome |
| `<服务器名>-ss` | Shadowsocks | 8388 TCP+UDP | aes-256-gcm(默认) |
| `<服务器名>-h2` | Hysteria2 | 8443 UDP | 自签证书(客户端跳过校验) |

## 客户端功能差异提示

| 能力 | Clash 系 | Hiddify | sing-box 官方 |
|---|---|---|---|
| 国内直连分流 | 内置(geodata) | 内置规则 | sing-box.json 默认全局,可按文档追加规则 |
| 广告拦截 | 需自行加规则集 | 内置 | 需自行加规则集 |
| 按应用分流 | TUN 模式 + 规则 | 内置 | 视平台 |
| 订阅更新 | URL 订阅 | URL 订阅 | 配置/订阅导入 |
| 延迟测速/自动选优 | AUTO 组(内置) | 内置 | auto 组(内置) |

## 场景速查

| 你的场景 | 怎么用 |
|---|---|
| 桌面三平台通用 | Clash Verge Rev 导入 clash.yaml,PROXY 组选 AUTO |
| 手机快速上网 | Hiddify 扫 links.txt 的 vless 链接 |
| iPhone 只有区外商店的第三方 app | 用 ss 节点链接(兼容性最广) |
| 家里软路由让全屋设备代理 | OpenClash 导入 clash.yaml 或订阅 |
| 命令行/服务器环境 | sing-box CLI 跑 sing-box.json,或 mihomo 跑 clash.yaml |
