# sing-box 官方客户端对接

sing-box 官方客户端与服务器同内核,协议支持最全(Reality 与 Hysteria2 全部可用)。
覆盖:macOS/iOS(SFI)、Android(SFA)、Linux/Windows(CLI)。

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

### 桌面 CLI(Linux/Windows/macOS)

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

- **TUN 模式**:官方 app 自带 TUN 开关(移动端连接页),桌面 CLI 如需 TUN
  按官方文档在配置中加 tun inbound(本仓库的 sing-box.json 不含 TUN,保持通用)。
- **导入后无法连接**:确认服务器安全组放行(见 deployment.md 第 3 步);
  连接日志报 timeout 优先查端口放行,报 TLS/Reality 握手失败查伪装站点可达性。
- **节点较多想分组**:移动端 app 内按节点名(hk-01-vless 等)分组即可。
