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

- `dist/sing-box.json`:通用版:无 TUN,CLI/移动端官方 app(SFI/SFA 自带 TUN 开关)导入即用
- `dist/sing-box-sfm.json`:macOS SFM,含 `platform.http_proxy`,SFM 仪表出现"系统HTTP代理"卡片(GUI 开关,实测可用)
- `dist/sing-box-sfw.json`:Windows SFW,含windows TUN 全接管
- `dist/sing-box-sfl.json`: Linux SFL,含 `auto_redirect`(Linux 官方推荐,nftables,需 root;不支持时删该字段即可)
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
2. 导入配置文件:官方桌面客户端均为纯内核(不自动注入 TUN),按平台选用
   TUN 版产物——macOS 用 `dist/sing-box-sfm.json`,Windows 用
   `dist/sing-box-sfw.json`,Linux 用 `dist/sing-box-sfl.json`
3. 选中刚导入的配置并启用,节点在 `proxy` 组中手动选择,或切 `auto` 自动选优

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
- `route`:分流形态(默认所有流量走 proxy;国内站点/内网直连;带
  `default_domain_resolver: "dns-direct"`(sing-box 1.12+ 对出站域名解析器的强制要求)
- `dns`:国内 223.5.5.5(UDP 直连)+ 1.1.1.1(DoH,经 proxy),防泄漏

## 分流规则(已默认内置,按需自改)

产物默认已集成国内直连分流,无需手动追加:route/rule_set 引用官方预编译
remote 规则集(首次启动自动下载,experimental.cache_file 落盘缓存,此后
离线可用)。

| 内置项 | 说明 |
|---|---|
| rule_set | geosite-geolocation-cn / geosite-geolocation-!cn / geoip-cn / geosite-category-ads-all(SagerNet 官方 srs,raw 直链) |
| dns.rules | 国内域名(geosite-geolocation-cn)解析走 dns-direct(223.5.5.5),防泄漏 |
| route.rules | 广告域名(geosite-category-ads-all)直接 reject;国内站点直连;geoip 兜底:目标 IP 属中国段且域名非明确外站时直连;其余走代理 |
| 规则集下载 | http_clients(tag rule-set-download,detour 指向 proxy):经代理隧道获取海外源,稳定;cache_file 缓存 |
| 版本要求 | sing-box 1.14+(http_clients 显式下载出站写法;隐式默认出站下载 1.14 弃用、1.16 移除,产物已用新写法规避迁移窗口) |

## FAQ

- **TUN 模式**:官方 app(移动端)自带 TUN 开关;桌面端(macOS SFM / Windows SFW /
  Linux SFL)同为纯内核,需要配置含 tun inbound 才会接管系统流量(并让 SFM
  仪表"系统HTTP代理"卡片可用)。
  > 实测结论(SFM 1.14.0 standalone,macOS,2026-09):tun inbound 在 SFM 的
  > NetworkExtension 环境可正常运行,但 **tun 地址必须避开本机局域网网段**——
  > 撞网段时启动报 `bind: can't assign requested address`(如局域网为
  > 172.19.0.0/19 时,默认示例地址 172.19.0.1 即撞车,换 172.18.0.1/30 即可)。
  > SFM 桌面端直接用产物 **`dist/sing-box-sfm.json`**(mixed+tun,地址已取
  > 172.18.0.1/30);若本机局域网恰为 172.18 段,把该地址改成其它未占用私有段
  > 再导入。连接后仪表出现"系统HTTP代理"卡片,开关由 SFM 管理系统代理
  > (实测可用,取代手动命令)。
- **SFM 连上后浏览器仍上不了外网(SFM 不接管系统流量)**:原因是不含 tun inbound
  的配置在 SFM 上只启动内核(mixed-in 监听 127.0.0.1:7890),系统流量仍直连。
  两种解法:
  ① 改用产物 `dist/sing-box-sfm.json`(推荐)——仪表出现"系统HTTP代理"卡片,
     点开开关即由 SFM 接管流量,断开时自动还原;
  ② 继续用 sing-box.json 时手动设置系统代理(网络服务名以
     `networksetup -listallnetworkservices` 为准),SFM 断开后需手动关闭,
     否则浏览器断网:
  ```bash
  networksetup -setsocksfirewallproxy Wi-Fi 127.0.0.1 7890
  networksetup -setwebproxy Wi-Fi 127.0.0.1 7890
  networksetup -setsecurewebproxy Wi-Fi 127.0.0.1 7890
  # 关闭(不使用时必须关,否则 SFM 断开后浏览器断网)
  networksetup -setsocksfirewallproxystatus Wi-Fi off
  networksetup -setwebproxystatus Wi-Fi off
  networksetup -setsecurewebproxystatus Wi-Fi off
  ```
  > macOS 实测:set*proxy 设置地址即同时启用系统代理(无需单独 status on,
  > 开启命令三条即可);关闭必须用 status off。
  > `"set_system_proxy": true` 只适用于 CLI/服务器等用户态场景(实测可用);
  > SFM 内该字段会导致启动失败(exit status 7,NE 沙盒无 networksetup 权限,
  > 参见 SagerNet/sing-box issue #3692),勿加。
- **导入后无法连接**:确认服务器安全组放行(见 deployment.md 第 3 步);
  连接日志报 timeout 优先查端口放行,报 TLS/Reality 握手失败查伪装站点可达性。
- **节点较多想分组**:移动端 app 内按节点名(hk-01-vless 等)分组即可。
