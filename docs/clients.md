# 客户端对接总览

服务端部署完成后,用 `dist/` 下四份分发物对接任意主流客户端。
每份分发物都包含全部服务器全部通道的节点(服务端 3 通道 × 服务器数)。

## 分发物与客户端的对应

| 分发物(dist/) | 内容 | 适用客户端 | 导入方式 |
|---|---|---|---|
| `links.txt` | 每行一个分享链接(vless:// ss:// hysteria2://) | 一切支持链接导入的客户端:sing-box 官方、Hiddify、移动端 app(Shadowrocket/Surge/Streisand 等) | 剪贴板粘贴 / 扫码 / 手动添加 |
| `clash.yaml` | Clash 订阅(节点 + PROXY/AUTO 组 + 国内直连分流) | Clash Verge Rev、mihomo、OpenClash、Clash Meta for Android 等 Clash 系 | 导入订阅文件 / URL |
| `sing-box.json` | sing-box 官方完整配置(mixed 入站 + auto/proxy 组 + 基础路由) | sing-box 官方客户端(SFI/SFA/CLI) | 配置文件导入 / 剪贴板 |
| `sub.txt` | base64 全链接订阅(机场标准格式) | 支持订阅的客户端(可作自托管订阅源内容) | 粘贴订阅内容或挂到任意静态 URL |

## 节点命名与协议

每个节点名 = `<服务器名>-<协议>`,如 `hk-01-vless` / `hk-01-ss` / `hk-01-h2`。

| 节点类型 | 分享链接 | Clash type | 端口 | 传输 |
|---|---|---|---|---|
| vless | `vless://`(Reality) | vless(reality-opts) | 443 TCP | tcp + vision flow |
| ss | `ss://` | ss | 8388 TCP+UDP | - |
| h2 | `hysteria2://` | hysteria2 | 8443 UDP | QUIC |

## 客户端能力与版本要求

| 客户端 | 平台 | vless+Reality | ss | hysteria2 | 备注 |
|---|---|---|---|---|---|
| sing-box 官方(SFI/SFA/CLI) | iOS/macOS(tvOS)/Android/Linux | 支持 | 支持 | 支持 | 与服务器同内核,最全 |
| Hiddify | Win/macOS/Linux/Android/iOS | 支持 | 支持 | 支持 | sing-box 内核 fork |
| Clash Verge Rev | Win/macOS/Linux | 支持(内核较新即可) | 支持 | 支持 | 需 mihomo 内核 v1.19+ 版本(2025 年后发布版均可) |
| mihomo 内核 | Linux/OpenWrt 等 | 支持 | 支持 | 支持 | 同上版本要求 |
| iOS 第三方(Shadowrocket/Surge/Stash 等) | iOS | 部分支持 Reality | 支持 | 部分支持 | 详见各 app 文档;最保守选 ss 节点 |

**分流规则说明**:`clash.yaml` 自带国内直连分流(GEOSITE,cn / GEOIP,CN),需客户端可加载
geodata(Clash Verge Rev/OpenClash 默认自带并自动更新,无需操作);
`sing-box.json` 默认全局代理形态(不依赖外置规则文件,官方 app 导入即用),
需要国内直连的按 [sing-box.md](clients/sing-box.md) 追加。

## 按平台选客户端

| 平台 | 推荐 | 备选 |
|---|---|---|
| Windows | Clash Verge Rev([clash-verge.md](clients/clash-verge.md)) | Hiddify、sing-box CLI |
| macOS | Clash Verge Rev | Hiddify、sing-box 官方(SFI) |
| Linux | Clash Verge Rev / mihomo([mihomo.md](clients/mihomo.md)) | sing-box CLI |
| Android | Hiddify([hiddify.md](clients/hiddify.md)) | sing-box(SFA)、Clash Meta for Android |
| iOS/iPadOS | Hiddify | sing-box(SFI)、Shadowrocket 等(区外 Apple ID 下载) |
| 网关盒子/软路由(OpenWrt) | OpenClash(mihomo 内核,[mihomo.md](clients/mihomo.md)) | 裸 mihomo / sing-box |

> 客户端本身自带分流域名与规则时(如 Hiddify 默认规则),无需重复在节点配置上做分流,
> 节点只管"连哪个服务器"。

各客户端分文档:

- [sing-box 官方](clients/sing-box.md)
- [Hiddify](clients/hiddify.md)
- [Clash Verge Rev](clients/clash-verge.md)
- [mihomo / OpenClash(网关盒子)](clients/mihomo.md)
