# sing-box 官方客户端对接

sing-box 官方客户端与服务器同内核,协议支持最全(Reality 与 Hysteria2 全部可用)。
官方图形客户端已覆盖全部主流平台(命名即缩写):

| 平台 | 官方客户端 | 获取渠道 |
|---|---|---|
| Android | SFA(sing-box for Android) | 官方 GitHub Releases(.apk)/Google Play |
| iOS/iPadOS | SFI(sing-box for iOS) | App Store(上架名 sing-box MT)/TestFlight |
| macOS | SFM(sing-box for macOS) | 官方 GitHub Releases(.pkg,Apple/Intel/Universal) |
| Windows | SFW(sing-box for Windows) | 官方 GitHub Releases(.exe) |
| Linux | SFL(sing-box for Linux) | 官方 GitHub Releases(.deb/.rpm/.pkg.tar.zst) |

另有 CLI 形态(`sing-box` 命令行)支持全部平台,适用于无图形界面的服务器/网关场景。

> 注:因 Apple 商店政策,SFM 不在 Mac App Store 分发,请从官方发布页获取;
> 桌面客户端与服务器同为锁版配套,导入本仓库生成的配置即可(见下)。

## 产物

- `dist/sing-box.json`:完整配置(全节点 + mixed 本地入站 7890 + auto 自动选择组 + proxy 手动组)
- `dist/links.txt`:单节点分享链接(vless:// hysteria2://)

## 导入方式

### 移动端(SFI iOS / SFA Android)

1. 把 `dist/sing-box.json` 传到手机(隔空投送/文件/网盘均可)
2. 打开 sing-box app,选择"从文件导入配置"(或"导入配置文件",不同版本文案略异),
   选中该 json 文件
3. 连接开关打开即用

或轻量方式:复制 `links.txt` 中任意一行的分享链接,app 内"从剪贴板导入",
逐个节点添加(适合只想用某通道的场景)。

### 桌面端(SFM macOS / SFW Windows / SFL Linux)

1. 从官方 GitHub Releases 下载对应平台的安装包(SFM 为 .pkg,SFW 为 .exe,
   SFL 为 .deb/.rpm)并安装
2. 把 `dist/sing-box.json` 作为配置文件导入(客户端内"导入配置/从文件添加",
   不同版本入口略异)
3. 选中刚导入的配置并启用,节点在 `proxy` 组中手动选择,或切 `auto` 自动选优

> 本仓库的 sing-box.json 不含 TUN 段;桌面官方客户端启用 TUN(全量接管)后同样可加载
> 该配置,无需改动。

### CLI(任意平台,含服务器/网关)

```bash
sing-box run -c sing-box.json
```

配置内含 `mixed-in` 本地入站(127.0.0.1:7890),系统/应用代理指向
`127.0.0.1:7890` 即可(HTTP 与 SOCKS5 同端口)。

## 配置说明

`sing-box.json` 结构:

- `inbounds.mixed-in`:本地 mixed 入站(HTTP+SOCKS5),端口 7890
- `outbounds`:direct + 各节点 + `auto`(urltest 延迟自动选优)+ `proxy`(selector 手动,默认首个节点)
- `route`:全局代理形态(默认所有流量走 proxy,内网/私网地址直连)
- `dns`:国内 223.5.5.5(UDP 直连)+ 1.1.1.1(DoH,经 proxy),防泄漏

## 分流:需要国内直连时

默认全代理会把国内流量也送出国(慢、费流量)。需要国内直连时,在 `route.rules` 里
追加 geosite 规则。方式任选:

**方式 A:引用远程规则集(需客户端能访问 GitHub)**

```jsonc
{
  "type": "rule_set",
  "rule_set": [
    { "tag": "geosite-cn", "type": "remote",
      "format": "binary", "url": "https://github.com/SagerNet/sing-geosite/releases/latest/download/geosite-cn.srs",
      "download_detour": "direct" }
  ],
  "invert": false, "action": "route", "outbound": "direct"
}
```

**方式 B:本地 .srs 文件(把 geosite-cn.srs 放配置同目录)**

```jsonc
{
  "type": "rule_set",
  "rule_set": [ { "tag": "geosite-cn", "type": "local", "format": "binary", "path": "geosite-cn.srs" } ],
  "action": "route", "outbound": "direct"
}
```

> 自用极简原则:能用默认全代理就跑通,规则追加按需进行。

## FAQ

- **TUN 模式**:官方 app(移动端)自带 TUN 开关;桌面端视客户端版本是否自带,
  需要 TUN 时按官方文档在配置中加 tun inbound(本仓库的 sing-box.json 不含 TUN,保持通用)。
- **导入后无法连接**:确认服务器安全组放行(见 deployment.md 第 3 步);
  连接日志报 timeout 优先查端口放行,报 TLS/Reality 握手失败查伪装站点可达性。
- **节点较多想分组**:移动端 app 内按节点名(hk-01-vless 等)分组即可。
