# mihomo / OpenClash 对接(Linux 与网关盒子)

mihomo 是 Clash 系内核(Meta 内核),面向无 GUI 场景与网关盒子(软路由/OpenWrt)。
无头使用场景分两类:

## 场景一:OpenWrt 网关盒子(整网代理,推荐 OpenClash)

OpenClash 是 OpenWrt 上最成熟的 mihomo 图形管理插件,局域网设备零配置全量代理。

1. OpenWrt 安装 OpenClash(软件源或离线 ipk)
2. 订阅配置:`dist/clash.yaml` 上传到路由器,或先自托管 `sub.txt` 用订阅 URL
   (OpenClash → 配置订阅 → 添加,本地文件放在 `/etc/openclash/config/` 后重启服务)
3. 启动后默认"规则模式",国内直连、国外走代理,局域网设备无需任何设置

> OpenClash 自带 geodata 与更新;核心版本保持较新以支持 vless+Reality/hysteria2 节点。

## 场景二:裸 mihomo(Linux 服务器/工控机)

```bash
# 安装 mihomo:到 https://github.com/MetaCubeX/mihomo/releases/latest
# 下载对应架构的 linux 包(如 mihomo-linux-amd64-vX.Y.Z.gz),解压后:
gunzip mihomo-linux-amd64-*.gz && chmod +x mihomo-linux-amd64-* && sudo mv mihomo-linux-amd64-* /usr/local/bin/mihomo

# 配置与运行
cp dist/clash.yaml /etc/mihomo/config.yaml
mihomo -d /etc/mihomo            # 默认监听 7890 混合端口
```

- 裸内核需自行准备 geodata:下载 `geoip.metadb` 与 `geosite.dat` 放入 `/etc/mihomo/`
  (MetaCubeX/meta-rules-dat releases;默认 geox-url 也可自动拉取,需能访问 GitHub)
- 系统代理或透明代理按 mihomo 文档接入(本仓库配置已含 mixed 入站 7890,
  应用指到 `127.0.0.1:7890` 即用)

## 分流规则

`clash.yaml` 内置分流规则(与产物 rules 段一致):

```text
GEOSITE,cn,DIRECT                # 国内域名直连
GEOIP,CN,DIRECT                  # 国内 IP 直连
GEOIP,private,DIRECT,no-resolve  # 内网地址直连(不解析域名)
MATCH,PROXY                      # 兜底走代理
```

规则依赖 geodata(GEOSITE/GEOIP 数据源):OpenClash 自带并自动更新;
裸 mihomo 需自行准备(见上文场景二)。自定义规则(如某域名强制直连)在
clash.yaml 的 rules 段按 Clash 语法追加后重启生效,或 OpenClash 用配置覆写。
切"全局模式"= 全部流量走代理(不分流),切回"规则模式"即恢复上述规则。

## 网关盒子替代思路:服务器端直接分流(可选)

如果盒子只是想要"局域网全局代理",也可以不装内核,直接在能跑 Docker 的盒子上
复用本仓库服务端形态,但这会把盒子变成代理出口,吞吐取决于盒子性能;
通常 OpenClash 方案更优,本仓库不做该形态的编排。

## 常见问题

- **vless 节点报错 unsupported**:内核版本过旧(Reality 需 2024+ 版本),升级 mihomo。
- **hysteria2 节点报错**:同上,hy2 支持需 v1.18+。
- **geodata 拉取失败(国内网络)**:手动下载放置,或用 gh-proxy 镜像替换 geox-url。
- **想全局模式(全流量代理)**:OpenClash 切"全局模式"即可,无需改配置。
