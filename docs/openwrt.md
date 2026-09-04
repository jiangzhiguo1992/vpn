# OpenWrt 网关盒子接入(裸 sing-box)

> 定位:软路由/网关盒子(OpenWrt)整网代理。本项目不依赖第三方插件,
> 盒子直接跑 sing-box 官方 OpenWrt 包,配置与桌面产物同源渲染
> (`dist/sing-box-openwrt.json`),一条命令部署、常驻免管。

## 1 方案形态

| 项 | 说明 |
|---|---|
| 配置产物 | `dist/sing-box-openwrt.json`(与 SFM/SFW/SFL 同源:同节点、同分流、同 DNS 规则) |
| 接管方式 | tun + auto_route + auto_redirect 全接管;auto_redirect 自动向 fw4 插入兼容规则,免手写防火墙(官方 Linux 推荐) |
| 入站 | 仅 tun(盒子无本机代理需求,故无 mixed 7890);双栈地址,IPv6 一并接管防泄漏 |
| DNS | dnsmasq 只留 DHCP,DNS 归 sing-box(auto_redirect 劫持 53 → 内置分流:国内直连 223.5.5.5、其余 DoH 经代理防泄漏) |
| 规则集下载 | legacy 形态(`rule_set[].download_detour: "proxy"`):兼容官方源内核(1.12+,见第 4 节),经代理隧道下载并落盘缓存 |
| 缓存 | `cache_file` 落盘 `/etc/sing-box/cache.db`(非 tmpfs,重启不丢) |
| 客户端设备 | 零配置:内网设备默认网关指向盒子即被接管,规则模式(国内直连/广告拦截/其余代理)与桌面一致 |

> 2026-09 状态:本变体参数依据 sing-box 官方 TUN 文档与 OpenWrt 社区实践,
> **未经 OpenWrt 真机验证**;部署后按[真机验证清单](#9-真机验证清单)逐项确认。

### 1.1 拓扑形态与架构(软路由 / 旁路由 / 二合一)

配置与部署脚本**不区分拓扑与 CPU 架构**——接管只认"流量是否经过盒子"。

**架构**:`sing-box-openwrt.json` 与部署脚本均架构无关;内核由部署脚本经官方源
自动安装,opkg/apk 按盒子架构(arm/aarch64/x86_64 等)选包,无需指定;主流架构
官方源均有 sing-box 包,极小众/过老架构缺失时会明确报错(见第 10 节 FAQ)。

**拓扑**:

| 形态 | 接入方式 | 说明与注意 |
|---|---|---|
| 软路由(主网关) | 盒子作主路由(拨号/DHCP),设备默认网关即盒子 | 零配置全接管,本文档主场景 |
| 旁路由(旁路网关) | 主路由继续拨号与 DHCP;**内网设备的网关与 DNS 都指向盒子**(或盒子开 DHCP、下发自身为网关+DNS) | 网关指向让流量经过盒子;DNS 必须同指盒子,否则国外域名经主路由 DNS 解析可能被污染/泄漏;fw4 默认允许 lan 转发、盒子不做 NAT(出站由主路由 masquerade),一般无需改防火墙 |
| 二合一路由 | 同软路由,叠加其它服务(AP/NAS/Docker 等) | 接管不受影响;注意 tun 网段与叠加服务网段(如 Docker bridge)不冲突(见第 7 节坑 2) |

**DNS 让位(脚本 9/9)在三种形态下的行为**:

- 盒子自己跑 DHCP(软路由,或旁路由形态由盒子下发):dnsmasq 正常让位,53 归 sing-box
- 盒子不开 DHCP(旁路由、主路由管 DHCP):dnsmasq 若仍在跑(仅本地解析、无
  DHCP),脚本正常让位,DNS 归 sing-box;若 dnsmasq 已停用/无配置则自动跳过——
  两种都不误伤。把 DNS 指向盒子的设备,其 53 流量经 auto_redirect 劫持进
  sing-box

**旁路由验证要点**:设备把网关+DNS 指向盒子后,按第 9 节清单逐项验收;访问
盒子/主路由管理页(LuCI)走局域网直连规则,不受接管影响。

## 2 前置要求

- 盒子:OpenWrt 23.05+(fw4);24.10(opkg)或 25.x(apk)均可,部署脚本自动识别包管理器
- 盒子已配 root 免密 SSH(部署走 BatchMode 非交互;首次连接自动接受指纹)
- 盒子可访问官方软件源(首次部署自动安装内核)
- 盒子 SSH 若为 dropbear(默认)且本机为 OpenSSH 9+:`scp` 默认走 SFTP 子系统,
  报 `subsystem request failed` 时先在盒子执行 `opkg install openssh-sftp-server`
  (部署失败提示里也有此指引)
- 本机已 `make gen`(产物含 `dist/sing-box-openwrt.json`)

## 3 一键部署

```bash
vpn openwrt -host root@192.168.1.1          # 常用形态
vpn openwrt -host 192.168.1.1 -p 2222       # user 缺省 root,自定义端口
```

脚本在盒子上依次做(幂等,可重复执行):

1. **前置检查**:上传的配置存在于 `/tmp` 并收紧为 0600(文件含全量节点凭据)
2. **装内核**:无 `sing-box` 时自动安装(`apk add`/`opkg install`,含 auto_route
   依赖 `ip-full`;`kmod-tun` 由包自动依赖);装后复检,仍不可用时报错并给出
   `opkg install --force-reinstall` / `apk fix` 指引(包记录已存在时会跳过补装)
3. **UCI/init 集成检查**:确认官方源包形态(`sing-box.main` + `/etc/init.d/sing-box`,
   官方源包安装时自带);手动放的 release apk 无此集成,会在此明确报错
   (请改用官方源/第三方源 ipk)
4. **语法校验**:`sing-box check -c` 校验上传配置——失败即停,**不触碰现网**,
   并打印内核版本首行供对照(内核过旧或语法不兼容时按第 4 节升级)
5. **备份**:现网 `/etc/sing-box/config.json`、`/etc/config/dhcp`、
   `/etc/config/sing-box` 留 `.bak`(**只保留首次部署前备份**,重跑不覆盖)
6. **落位**:配置写入 `/etc/sing-box/config.json`(0600),随即删除 `/tmp` 上传件
7. **启用**:UCI `sing-box.main.user='root'`(TUN 需 root 运行)+ `enabled='1'`
   + `/etc/init.d/sing-box enable`
8. **启动**:`/etc/init.d/sing-box restart`
9. **DNS 让位**:dnsmasq `port='0'`(仅禁 DNS,DHCP 保留;dnsmasq 停用时不被
   意外拉起)并重启,打印原端口供恢复

最后验证:**轮询最长 30 秒**等进程起来(弱盒子启动慢不误报);重启前脚本先打日志
锚,随后只查**本次启动后**有无 **FATAL**(logd 环形缓冲不清空,历史 FATAL 不误报;
进程在 ≠ 健康:规则集首启下载失败/tun 创建失败都会 FATAL,脚本会明确报错而非
误报成功)。若为首启且规则集需在线下载,启动可能超过 30 秒,稍后重跑一次即可。
输出 `✓ OpenWrt 网关 sing-box 部署完成` 即完成;再按第 9 节真机验证。

## 4 内核版本与升级

OpenWrt 官方源版本滞后于上游(24.10 与 25.12 两稳定分支独立打包,版本各自演进):

| 源 | sing-box 版本 |
|---|---|
| OpenWrt 24.10 稳定 | 1.12.22-r1 |
| OpenWrt 25.12 稳定 | 1.12.17-r1 |
| packages master | 1.13.x |
| 上游最新 | 1.14+ |

- **兼容性**:`sing-box-openwrt.json` 的规则集下载用 legacy 形态
  (`download_detour`,1.12 起支持),不含 1.14 新增的 `http_clients` 字段——
  官方源内核(1.12/1.13 对未知字段直接 FATAL 拒启)开箱即用,无需换源
- **未来注记**:`download_detour` 在 1.14 弃用、计划 1.16 移除;届时官方源早已
  跟进(桌面产物也已在用 1.14 `http_clients` 形态),OpenWrt 变体整体切回新
  形态即可
- **升级内核**:官方源内 `opkg/apk upgrade sing-box` 即可(配置不受影响);
  要官方源之外的较新内核:PassWall 构建源(openwrt-passwall-build,打包较新
  版本,含 UCI/init 集成)或 sing-box 官方 releases 的 OpenWrt apk(1.14+ 起
  提供,但无 init/UCI 集成,不推荐裸用——部署会死在上述第 3 步检查)
- 官方包 conffiles 保护 `/etc/config/sing-box` 与 `/etc/sing-box/`,升级保留
  配置与缓存

## 5 DNS 架构(为什么让位 dnsmasq)

sing-box 作者明确建议不要把它放在 dnsmasq 后面(防泄漏,见 SagerNet/sing-box#3705):
dnsmasq 的 DNS 停用(`port='0'`),sing-box 经 auto_redirect 接管 53:

```text
内网设备 DNS 请求 → 盒子 53 → auto_redirect DNAT 到 tun → hijack-dns
  → sing-box dns 模块:geosite-cn → 223.5.5.5 直连
                     其余        → DoH(1.1.1.1,经代理隧道,防泄漏)
```

影响与恢复:

- 盒子本地域名解析(.lan 主机名)不可用,LuCI 等用 IP 访问即可
  (旁路由形态:仅指向盒子的设备受影响,主路由与其它设备不受影响)
- 恢复 dnsmasq DNS:执行部署时打印的原值命令(`uci set dhcp.@dnsmasq[0].port='<原值>'`
  `&& uci commit dhcp && /etc/init.d/dnsmasq restart`);原状未显式配置端口时
  可 `uci delete dhcp.@dnsmasq[0].port && uci commit dhcp`;备份在
  `/etc/config/dhcp.bak`

## 6 更新节点/配置

节点变更后重跑 `make gen`,然后重复执行第 3 节命令即可(幂等:先校验、再备份、后落位)。

## 7 已知限制与坑

1. **auto_redirect 与既有 NAT/端口转发**:sing-box 向 fw4 插入的 nftables 规则
   可能影响端口转发(openwrt/packages#25105、SagerNet/sing-box#2167,2025 年仍有复现)。
   盒子有端口映射需求时**必须真机验证**;冲突时可临时关闭配置里 `auto_redirect`
   (仅留 auto_route,语义弱一些但冲突面小)
2. **tun 网段撞内网**:默认 `172.18.0.1/30` + `fdfe:dcba:9876::1/126`,与内网网段重叠会
   启动失败/路由异常;改盒子 `/etc/sing-box/config.json` 的 `inbounds[0].address`
   后 restart
3. **规则集首启下载**:remote rule-set 首次启动需经代理隧道下载,节点不可达时启动
   失败(FATAL,部署脚本会检出并报错);成功后落盘 `/etc/sing-box/cache.db`,
   此后离线可用。首启失败:先确认节点可达,或把本仓库 `srs/` 下离线 .srs 拷到
   盒子并把对应 rule_set 改 `type: local`
4. **防环**:变体已内置 `route.auto_detect_interface`;仍出现"流量被吸入 TUN 自环"
   (症状:大面积超时)时查 `logread | grep sing-box`,确认无本地 DNS 指向 127.0.0.1 残留
5. **IPv6**:双栈接管;内网无 IPv6 无影响;想禁用 v6 代理时删配置里 address 的 v6 段即可。
   注意内置 DNS `strategy: ipv4_only`:纯 AAAA(无 A 记录)站点不可达,现实中罕见;
   确有需求时手动把盒子配置的 dns strategy 改为 `ipv4_first`

## 8 回滚

```bash
# 1. 先撤代理接管(顺序很重要:先停 sing-box,再恢复 dnsmasq)
/etc/init.d/sing-box stop

# 2. 恢复配置与 UCI(用部署时的备份整体还原,enabled/user 原值一并恢复)
cp /etc/sing-box/config.json.bak /etc/sing-box/config.json    # 如备份存在
cp /etc/config/sing-box.bak /etc/config/sing-box              # 如备份存在
uci set dhcp.@dnsmasq[0].port='<部署输出中的原值>' && uci commit dhcp
#   (原状未显式配置端口:uci delete dhcp.@dnsmasq[0].port && uci commit dhcp)

# 3. 恢复 DNS 与停用服务(部署前若服务本就启用,再 enable 后 restart 即可)
/etc/init.d/dnsmasq restart
/etc/init.d/sing-box disable
```

> 备份语义:`.bak` 只保留**首次部署前**的状态(重跑部署不覆盖)。两轮部署之间
> 手动改过配置的,回滚会回到首次部署前——需要以当前配置为新基准时先自行备份。

## 9 真机验证清单

- [ ] 内网设备(手机/PC)重连网络后:国内站点直连、国外站点可访问
- [ ] IPv6 站点可访问(双栈接管生效)
- [ ] DNS 无泄漏:用 dnsleaktest.com 等查解析出口与访问一致
- [ ] 盒子重启后 sing-box 自启(`logread | grep sing-box` 确认无手动干预)
- [ ] 盒子有端口转发/DDNS 时:规则仍生效(auto_redirect 冲突项)
- [ ] 重跑 `vpn openwrt` 幂等,配置刷新生效;部署输出无 FATAL 检出
- [ ] (旁路由形态)设备网关与 DNS 均指向盒子后:被接管设备国内外正常;未指向
      盒子的设备(仍走主路由)不被代理——分流边界符合预期
- [ ] (旁路由形态)DHCP 唯一:盒子与主路由不同时下发 DHCP(双网关冲突);
      由主路由下发时,盒子 dnsmasq 的 DHCP 应关闭(让位步骤只禁 DNS 不禁 DHCP,
      需手动在 dhcp 配置停用或删除 LAN 的 DHCP 服务)

## 10 排障 FAQ

| 症状 | 排查 |
|---|---|
| 部署脚本报 FATAL 检出 | `logread \| grep sing-box`:多为规则集首启下载失败(节点不可达)或 tun 创建失败(网段冲突/缺权限),见第 7 节 |
| 全部不通 | `logread \| grep sing-box` 查错误;先 `sing-box check -c /etc/sing-box/config.json` |
| 国内通、国外不通 | 隧道握手失败:节点变更/服务器不可达;`logread` 查 ERROR,换 auto 组节点或重跑部署 |
| 网页打不开但 ping 通 | DNS 链路问题:确认 dnsmasq 已让位(第 5 节)、53 被 sing-box 接管 |
| (旁路由形态)国外站不通/解析异常 | 设备 DNS 未指向盒子,DNS 查询走主路由可能被污染;把网关与 DNS 都指向盒子(见 1.1 节) |
| 大面积超时/疑似自环 | 见第 7.4:查本地 DNS 残留与 sing-box 日志 |
