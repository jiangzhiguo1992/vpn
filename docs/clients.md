# 客户端对接总览

服务端部署完成后,用 `dist/` 下四份分发物对接任意主流客户端。
每份分发物都包含全部服务器全部通道的节点(每服务器 3 通道)。
平台选型(哪个平台用哪个客户端)见根 README 第 3 节"平台与客户端生态"。

## 1 分发物与客户端的对应

| 分发物(dist/) | 内容 | 适用客户端 | 导入方式 |
|---|---|---|---|
| `links.txt` | 每行一个分享链接(vless:// hysteria2://) | 一切支持链接导入的客户端:sing-box 官方、Hiddify、移动端 app 等 | 剪贴板粘贴 / 扫码 / 手动添加 |
| `clash.yaml` | Clash 订阅(节点 + PROXY/AUTO 组 + 国内直连分流) | Clash Verge Rev、mihomo、OpenClash、Clash Meta for Android 等 | 导入订阅文件 / URL |
| `sing-box.json` | sing-box 官方完整配置(mixed 入站 + auto/proxy 组 + 基础路由) | sing-box 官方客户端(SFI/SFA/CLI) | 配置文件导入 / 剪贴板 |
| `sub.txt` | base64 全链接订阅(机场标准格式) | 支持订阅的客户端(可作自托管订阅源内容) | 粘贴订阅内容或挂到任意静态 URL |

## 2 节点命名与协议

节点名 = `<服务器名>-<协议短名>`(如 `hk-01-vless`),同一服务器两个通道即两个节点:

| 节点名 | 协议 | 端口 | 传输 | 定位 |
|---|---|---|---|---|
| `<名>-vless` | VLESS+Reality | 443 TCP | tcp + vision flow | 主力,抗封锁 |
| `<名>-h2` | Hysteria2 | 8443 UDP | QUIC | 逃生/提速 |

平时用 vless 节点即可;vless 被封锁时切 h2 节点是天然容灾。

## 3 分流规则说明

- **clash.yaml** 内置国内直连分流(GEOSITE,cn → GEOIP,CN → GEOIP,private → MATCH),
  需客户端可加载 geodata(Clash Verge Rev / OpenClash 默认自带并自动更新,无需操作)
- **sing-box.json** 默认全局代理 + 内网直连形态(不依赖外置规则文件,官方 app 导入即用),
  需要国内直连的按 [sing-box.md](clients/sing-box.md) 追加规则
- 客户端本身自带分流规则的(Hiddify 默认规则、Clash 系内核规则),节点只管连哪个服务器,无需重复配置

## 4 常见问题

**Q:节点名里的 vless/ss/h2 是什么?**
同一服务器三个通道的节点,见第 2 节;平时用 vless,某通道故障时切换其他节点是天然容灾。

**Q:移动端(iOS/Android)推荐哪个?**
Hiddify(全平台、自带分流、链接导入最顺);iOS 无 Hiddify 条件时用区外商店的
sing-box(SFI)或 Shadowrocket 等第三方 app,第三方 app 不支持 Reality 时选 h2 节点。

**Q:多服务器时怎么自动选最优节点?**
clash.yaml 的 AUTO 组与 sing-box.json 的 auto 组都是延迟自动选优(urltest),
把常用服务器设为 PROXY 默认即可;所有节点在同一配置里随时切换。

**Q:节点信息变更后客户端要全部重配吗?**
本方案节点变更频率极低。加服务器:重新 `make gen`,把新增节点链接发给需要的人;
换服务器 IP:重新导入受影响服务器节点即可(链接内地址更新,凭据不变,无需重配其他设备)。

**Q:分发物怎么更新?**
本地文件导入无更新概念,节点变更时重新 `make gen` 后重新导入;
需要在线更新时把 `sub.txt` 托管到静态 URL,用订阅 URL 方式导入。

**Q:clash.yaml 规则报错/缺 geodata、sing-box.json 不分流?**
分别见 [clash-verge.md](clients/clash-verge.md) 与 [sing-box.md](clients/sing-box.md) 的对应章节。

## 5 客户端分文档

| 客户端 | 文档 | 覆盖平台 |
|---|---|---|
| sing-box 官方 | [clients/sing-box.md](clients/sing-box.md) | iOS(SFI)/macOS/Android(SFA)/Linux 与桌面 CLI |
| Hiddify | [clients/hiddify.md](clients/hiddify.md) | Windows/macOS/Linux/Android/iOS |
| Clash Verge Rev | [clients/clash-verge.md](clients/clash-verge.md) | Windows/macOS/Linux |
| mihomo / OpenClash | [clients/mihomo.md](clients/mihomo.md) | Linux 与网关盒子(OpenWrt) |
