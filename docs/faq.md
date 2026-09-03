# FAQ(常见问题)

## 连接与部署

**Q:客户端全部连不上,怎么排查?**
按顺序:1) 服务器 `docker compose -f /opt/sing-box/docker-compose.yml ps` 是否 Running;
2) 日志是否三行 inbound started(vless-in/ss-in/hy2-in);3) 云安全组与系统防火墙是否放行
(443/TCP、8388/TCP+UDP、8443/UDP,与实际配置端口一致);4) 客户端节点信息与 `dist/links.txt`
是否一致(换过服务器 IP 后需要重新导入)。

**Q:VLESS 连不上,但 ss/h2 正常?**
伪装站点(server_name)不可达被墙,换一个(www.apple.com / www.microsoft.com / www.amazon.com 等
实测选择);或 443 端口被服务器其他程序占用。Reality 每次握手实时向伪装站点要证书,
伪装站点本身必须能被服务器访问。

**Q:H2 连不上,其他正常?**
H2 是纯 QUIC,只走 UDP。检查 8443/UDP 是否在云安全组放行(只放 TCP 必失败)。
客户端所在网络若封锁 UDP,Hy2 本来就用不了,这是设计上的逃生通道,不是主通道。

**Q:deploy 时拉镜像失败/超时?**
deploy.sh 自动回退 docker.io 备源;仍失败就在服务器手动
`docker pull docker.io/sagernet/sing-box:v1.14.0` 后重跑 deploy(镜像已在本地则跳过拉取)。

**Q:提示配置语法校验失败?**
升级了镜像版本(config.json 模板与新版 schema 不兼容)或手工改过 config.json。
查看 check 输出定位字段;恢复方法:重新 `make gen` 还原产物后 deploy。

**Q:某服务器部署失败,会影响其他服务器吗?**
不会。deploy 逐台执行,失败即停(报错含服务器名);修复后重跑,已成功的服务器跳过拉取直接重部署。

## 证书

**Q:自签证书安全吗?会被中间人吗?**
H2 自签证书场景客户端配置了 insecure(跳过证书校验),理论上存在被中间人截获的风险面;
Reality 通道(主力)不依赖证书,基于公钥体系,无此问题。要消除 H2 风险面:
用受信证书(清单 `hysteria2.server_name` + 放置 cert.pem/key.pem,见 deployment.md"受信证书")。

**Q:自签证书会过期吗?**
cert.sh 生成 10 年有效期;到期后服务器上重跑 `sh cert.sh` 重新生成即可,客户端零改动
(insecure 跳过校验,换证书无感知)。用 Let's Encrypt(90 天)则自行配置 certbot renew。

**Q:证书 SNI 是什么?为什么清单里 hysteria2.server_name 一般不填?**
SNI 是 TLS 握手时客户端声明的"要访问的域名"。自签场景服务器只认证书文件、不校验 SNI,
客户端跳过校验 → 不需要填。仅当使用受信证书时填证书域名(客户端会校验 SNI=证书域名)。

## 清单与凭据

**Q:servers.json 与 example-servers.json 什么关系?**
example 是模板(提交到 git);servers.json 是你的实际清单,gen 会回填凭据,
**不要提交到 git**(已 gitignore)。备份凭据 = 备份 servers.json。

**Q:重跑 make gen 会换密钥吗?**
不会。凭据回填幂等(非空不重新生成),重跑产物不变。误删 servers.json 后凭据才会翻新,
此时所有已分发客户端需要重新导入(旧节点失效)。

**Q:字段填错会怎样?**
校验在生成前拦截(含拼错字段名的严格模式),报错指明字段;不会产出半成品配置。

**Q:服务器被墙/IP 被封怎么办?**
换 IP:云控制台换新 IP 或销毁重建 → 改清单 address → `make gen && make deploy` →
客户端重新导入(仅地址变化,凭据不变)。**封端口不封 IP**:改对应通道端口 + 云安全组。

## 客户端

**Q:节点名里的 vless/ss/h2 是什么?**
同一服务器三个通道的节点:Reality(主力,443)/SS(保底,8388)/Hy2(逃生,8443)。
平时用 vless 即可;某通道故障时切换其他节点是天然容灾。

**Q:clash.yaml 导入后提示规则错误/缺 geodata?**
Clash Verge Rev 与 OpenClash 自带 geodata 自动更新,不需操作;裸 mihomo 需自行放置
geoip.metadb/geosite.dat(见 clients/mihomo.md)。国内网络拉取失败时换镜像源。

**Q:sing-box.json 导入后所有流量都走代理(不分流)?**
设计如此(全局代理形态,零外置依赖)。需要国内直连按 clients/sing-box.md 追加规则;
或用 Hiddify/Clash 系(自带分流规则)。

**Q:移动端(iOS/Android)推荐哪个?**
Hiddify(全平台、自带分流、链接导入最顺);iOS 无 Hiddify 条件时用区外商店的
Shadowrocket 等,保底用 ss 节点链接。

**Q:节点信息变更后客户端要全部重配吗?**
本方案节点变更频率极低(换服务器/加服务器)。加服务器:重新 gen,把新节点链接发给需要的人;
换 IP:重新导入受影响服务器节点即可(链接内地址更新,凭据不变)。

## 运维与升级

**Q:如何升级 sing-box 镜像版本?**
改 `internal/server/render.go` 的 ImageVersion 常量 → `make gen && make deploy`。
凭据不变,客户端无感;大版本升级后实测三通道连通(模板与 schema 强相关)。

**Q:如何给服务器加受信证书(Let's Encrypt)?**
见 deployment.md"受信证书"章节:域名解析 → certbot 签发 → 清单填 server_name →
证书命名 cert.pem/key.pem 放入产物目录 → deploy。

**Q:多服务器时客户端怎么选最优节点?**
clash.yaml 的 AUTO 组与 sing-box.json 的 auto 组都是延迟自动选优(urltest),
把最常用服务器设为 PROXY 默认即可。所有节点在同一配置里,随时切换。

**Q:家里软路由(OpenWrt)怎么让全屋设备都走代理?**
OpenClash 导入 clash.yaml(或托管 sub.txt 做订阅),默认规则模式:国内直连、国外代理,
全屋零配置。详见 clients/mihomo.md。
