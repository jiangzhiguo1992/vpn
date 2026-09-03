# Clash Verge Rev 对接(Windows / macOS / Linux)

> 官网:<https://www.clashverge.dev/> | GitHub:<https://github.com/clash-verge-rev/clash-verge-rev>

Clash Verge Rev(内核 mihomo)是桌面三平台最常用的 Clash 系客户端,
自带 geodata(geoip/geosite 自动更新)与可视化规则管理。
需较新内核版本(2025 年后发布的常规版本即可):vless+Reality、hysteria2 均支持。

## 导入

使用 `dist/clash.yaml`(节点 + PROXY/AUTO 组 + 国内直连分流,开箱即用):

1. 打开 Clash Verge Rev,左侧"订阅"(Profiles)
2. 点右上角"新建",选择"导入"(Import):
   - 方式 A:拖入 `dist/clash.yaml` 文件
   - 方式 B:若把 `sub.txt` 托管到静态 URL,填 URL 用订阅方式(支持在线更新)
3. 选中该订阅,左侧切到"代理"页确认节点出现在列表中
4. 打开"系统代理"开关(或"TUN 模式"全量接管),选节点:

| 组 | 说明 |
|---|---|
| PROXY | 手动选择:选具体节点,或选 AUTO 让它自动挑延迟最优 |
| AUTO | url-test 自动选择(每 300s 测一次 www.gstatic.com 延迟) |

## 分流规则

`clash.yaml` 已内置分流规则(与产物 rules 段一致,开箱即用无需操作):

```text
GEOSITE,cn,DIRECT            # 国内域名直连
GEOIP,CN,DIRECT              # 国内 IP 直连
GEOIP,private,DIRECT,no-resolve  # 内网地址直连(不解析域名)
MATCH,PROXY                  # 兜底走代理
```

Verge Rev 首次运行会自动下载 geodata(需能访问 GitHub,或在国内镜像源设置里换源)。
规则偏好(如某域名强制代理/直连)在"订阅 → 右键 → 编辑文件"里按 Clash 规则语法追加即可。

## 常见问题

- **导入后节点全部超时**:先看服务器安全组(端口 443/8443);
  再在"订阅"里点该订阅的右键菜单"更新"确认能加载;
  单节点测速可用"代理"页的延迟测试按钮。
- **想只走某个通道**:PROXY 组手动选对应节点即可;ss/h2 节点同样在列表里。
- **订阅更新**:本地文件导入无更新概念,节点变更时重新 `make gen` 后重新导入;
  用 URL 订阅方式(托管 sub.txt)则可在客户端内一键更新。
- **geodata 下载失败**:Verge Rev 设置里把 Geo 数据源换成国内镜像
  (如 `https://gh-proxy.com/https://github.com/MetaCubeX/meta-rules-dat/...`),
  或手动放置 geoip.metadb / geosite.dat 到数据目录。
