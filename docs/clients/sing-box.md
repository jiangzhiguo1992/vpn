# sing-box 官方客户端对接

> 官网:<https://sing-box.sagernet.org/zh> | GitHub:<https://github.com/SagerNet/sing-box>

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
- `route`:全局代理形态(默认所有流量走 proxy,内网/私网地址直连);带
  `default_domain_resolver: "dns-direct"`(sing-box 1.12+ 对出站域名解析器的强制要求)
- `dns`:国内 223.5.5.5(UDP 直连)+ 1.1.1.1(DoH,经 proxy),防泄漏

## 分流规则

默认全代理会把国内流量也送出国(慢、费流量)。注意 sing-box **不内置任何分流数据**:
`geosite`/`geoip` 规则字段已在 sing-box 1.12 移除,国内直连必须用 **rule-set 规则集**
实现——先在 `route.rule_set` 声明规则集(引用官方预编译的 `.srs`),再在路由/DNS 规则里按标签引用。

数据源:SagerNet 官方 sing-geoip(https://github.com/SagerNet/sing-geoip) / sing-geosite(https://github.com/SagerNet/sing-geosite) 仓库每次 release 自动把数据编译成按分类
拆分的 `.srs`,发布在 `rule-set` 分支,raw 直链即用(release 只发 `.db`,没有 `.srs`,
不要用 release/latest/download 链接):

| 规则集 | raw URL | 用途 |
|---|---|---|
| geosite-geolocation-cn | https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-cn.srs | 中国区域名 |
| geoip-cn | https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs | 中国 IP 段 |
| geosite-cn(可选,类别更宽) | https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs | 中国相关站点 |

以下片段基于产物内已有字段(dns-direct / dns-proxy / proxy 组),对 `dist/sing-box.json`
追加三处(重跑 `make gen` 会覆盖产物,长期需要请改造生成模板)。

**方式 A:remote 远程规则集(推荐,客户端自动下载并缓存,只需一次联网)**

1) `dns` 段加 `rules`(国内域名解析走直连 DNS,防泄漏):

```jsonc
"dns": {
  ...,
  "rules": [
    { "rule_set": "geosite-geolocation-cn", "server": "dns-direct" }
  ]
}
```

2) `route` 段加声明与规则(中国区域名直连;再兜底:目标 IP 属中国段且域名非明确外站时直连):

```jsonc
"route": {
  "default_domain_resolver": "dns-direct",
  "rule_set": [
    { "type": "remote", "tag": "geosite-geolocation-cn", "format": "binary",
      "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-cn.srs",
      "download_detour": "direct" },
    { "type": "remote", "tag": "geosite-geolocation-!cn", "format": "binary",
      "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-!cn.srs",
      "download_detour": "direct" },
    { "type": "remote", "tag": "geoip-cn", "format": "binary",
      "url": "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs",
      "download_detour": "direct" }
  ],
  "rules": [
    { "action": "sniff" },
    { "protocol": "dns", "action": "hijack-dns" },
    { "ip_is_private": true, "outbound": "direct" },
    { "rule_set": "geosite-geolocation-cn", "action": "route", "outbound": "direct" },
    { "type": "logical", "mode": "and",
      "rules": [
        { "rule_set": "geoip-cn" },
        { "rule_set": "geosite-geolocation-!cn", "invert": true }
      ],
      "action": "route", "outbound": "direct" }
  ],
  "final": "proxy"
}
```

3) 顶层加 `experimental`(remote 规则集缓存的必需前提,缺了每次启动都重新下载):

```jsonc
"experimental": {
  "cache_file": { "enabled": true }
}
```

说明:`download_detour: "direct"` 让规则集下载直连,不依赖代理隧道先可用;sing-box 1.14
起该字段告警 deprecated(1.16 移除),将由 `http_clients` 机制取代,届时按官方文档迁移。
**首次启动必须能下载成功**:实测(sing-box 1.14)无缓存时初始下载失败会直接
`FATAL` 启动失败;下载成功后 cache_file 落盘,此后启动不再依赖网络。GitHub raw 直连
不稳时,URL 前加镜像前缀(如 `https://ghfast.top/`);sing-box 1.14+ 也可给 remote
规则集配 `initial_path` 预置本地初始文件,启动不被下载阻塞。

**方式 B:local 本地文件(离线可用;适合 CLI/桌面,移动端 app 沙盒内放不进文件,不适用)**

下载 `.srs` 到本机,`path` 指向绝对路径:

```jsonc
"route": {
  "rule_set": [
    { "type": "local", "tag": "geosite-geolocation-cn", "format": "binary",
      "path": "/etc/sing-box/geosite-geolocation-cn.srs" },
    { "type": "local", "tag": "geoip-cn", "format": "binary",
      "path": "/etc/sing-box/geoip-cn.srs" }
  ],
  // ... rules / final 同方式 A 第 2) 步(规则里去掉 download_detour 相关字段)
}
```

**方式对比**

| 维度 | A remote | B local |
|---|---|---|
| 数据更新 | 自动(默认 1d 检查) | 手动下载替换 |
| 首次联网 | 需要(下载失败则内核启动失败,已缓存后不再依赖) | 不需要 |
| 适用客户端 | 全部官方客户端 | CLI/桌面(移动端无法放置文件) |
| GitHub 不可达时 | 无缓存首启 FATAL;已有缓存不受影响 | 不受影响 |

> 自用极简原则:能用默认全代理就跑通,规则追加按需进行;加完先 `sing-box check -c
> sing-box.json` 校验,启动后看日志确认 rule_set 加载成功,再实测国内站点(直连、延迟低)
> 与国外站点(走代理)。

## FAQ

- **TUN 模式**:官方 app(移动端)自带 TUN 开关;桌面端视客户端版本是否自带,
  需要 TUN 时按官方文档在配置中加 tun inbound(本仓库的 sing-box.json 不含 TUN,保持通用)。
- **导入后无法连接**:确认服务器安全组放行(见 deployment.md 第 3 步);
  连接日志报 timeout 优先查端口放行,报 TLS/Reality 握手失败查伪装站点可达性。
- **节点较多想分组**:移动端 app 内按节点名(hk-01-vless 等)分组即可。
