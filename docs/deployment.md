# 服务端部署:从 0 到 1

目标:在你的一台海外服务器上跑起 sing-box,并把全部客户端对接产物生成本地。
全程只依赖:本地电脑(Go 1.27+、ssh/scp)+ 一台海外 VPS。服务器上不需要装任何 Go 环境。

完成后你将拥有:

| 通道 | 端口 | 协议 | 用途 |
|---|---|---|---|
| VLESS+Reality | 443(TCP) | 伪装 TLS 流量,抗封锁主力 | Clash 系 / sing-box 系 / Hiddify 全支持 |
| Shadowsocks | 8388(TCP+UDP) | 经典 SS,最广兼容 | 任何客户端保底可用 |
| Hysteria2 | 8443(UDP/QUIC) | 自签证书,逃生/提速 | 网络受限时的备选通道 |

---

## 第 0 步:本地环境自检(一次性)

macOS / Linux / Windows(10 1809+,需启用内置 OpenSSH 客户端)均可,只需:

| 工具 | 用途 | 检查 |
|---|---|---|
| Go 1.27+ | 编译本项目(纯标准库,零第三方依赖) | `go version` |
| ssh / scp | 部署推送 | `make doctor` 自动检查 |
| openssl | 服务器上生成自签证书(可选,服务器侧需要) | `make doctor` 自动检查 |

```bash
cd <本项目目录>
make doctor    # 全绿即可继续
```

国内网络无需额外配置(GOPROXY 只影响第三方依赖拉取,本项目无第三方依赖)。

## 第 1 步:准备服务器(一次性)

**一台海外 VPS**,Debian/Ubuntu 系(脚本自动处理 Docker 安装),纯净 IP(未被墙)。
商家选择社区共识参考(价格以官网为准):

| 商家 | 定位 | 参考价格 | 特点 |
|---|---|---|---|
| Vultr / DigitalOcean | 主力 | $6/月级 | 大厂稳定,东京/新加坡延迟低;被封 IP 可销毁重建换新 |
| BandwagonHost | 优化线路 | $50/年级 | CN2 GIA 线路面向中国用户,晚高峰稳 |
| RackNerd | 备用 | $10-15/年 | 便宜,美西普通线路 |
| Oracle / Google 免费层 | 零成本验证 | 免费 | 适合先跑通流程,流量有限制 |

**服务器侧需满足**(其余全部由 deploy.sh 自动处理):

1. SSH 可登录(root 或可 sudo 用户)
2. 云安全组/防火墙放行入站(见第 3 步,最容易漏的一步)
3. IPv6 可选:需要国外 v6 目标可达时给实例分配 v6 地址(清单 address 填 v6 即可)

## 第 2 步:SSH 免密登录配置(一次性,deploy 的前提)

`make deploy` 全程非交互,需要本机到服务器的免密 SSH:

```bash
# 1. 生成密钥(默认 ~/.ssh/id_ed25519;提示 passphrase 直接回车=空密码)
ssh-keygen -t ed25519

# 2. 拷贝公钥到服务器(输入一次密码后即免密;或到云控制台粘贴公钥)
ssh-copy-id root@服务器IP
```

验证:`ssh root@服务器IP "uname -a"` 能直接输出(不再问密码)即可。

## 第 3 步:填写服务器清单(每台服务器一段)

```bash
cp example-servers.json servers.json
```

编辑 `servers.json`,一台服务器一段,格式如下(字段逐项说明见下表):

```json
{
  "servers": [
    {
      "name": "hk-01",
      "location": "香港",
      "address": "1.2.3.4",
      "ssh": { "user": "root", "port": 22 },
      "vless": { "port": 443, "server_name": "www.apple.com" },
      "shadowsocks": { "port": 8388 },
      "hysteria2": { "port": 8443 }
    }
  ]
}
```

### 字段说明

| 字段 | 必填 | 类型 | 说明 |
|---|---|---|---|
| `name` | 是 | 身份 | 服务器唯一标识(如 `hk-01`),同时是产物目录名与客户端节点名;仅允许字母/数字/`._-` |
| `location` | 否 | 身份 | 显示用地区(如"香港"),透传到客户端节点列表 |
| `address` | 是 | 身份 | 客户端连接地址:域名或裸 IP。**IP 直连最简单**(推荐个人场景);IPv6 直接填裸地址(如 `2001:db8::1`);**不要带端口**(`1.2.3.4:443` 会被拒绝) |
| `ssh.user` | 否 | 身份 | 部署用户,默认 `root`(仅 deploy 用) |
| `ssh.port` | 否 | 身份 | SSH 端口,默认 `22`(仅 deploy 用) |
| `vless.port` | 否 | 身份 | VLESS 监听端口,默认 `443` |
| `vless.server_name` | 是(有 vless 时) | 身份 | 伪装站点(Reality 握手目标),推荐 `www.apple.com`(实测可用) |
| `shadowsocks.port` | 否 | 身份 | SS 监听端口,默认 `8388`(TCP+UDP 同端口) |
| `shadowsocks.method` | 否 | 身份 | 加密方式,默认 `aes-256-gcm`;可选 `aes-128-gcm` / `chacha20-ietf-poly1305` / `xchacha20-ietf-poly1305` |
| `hysteria2.port` | 否 | 身份 | H2 监听端口,默认 `8443`(仅 UDP) |
| `hysteria2.server_name` | 否 | 身份 | 证书域名,默认不填(=IP+自签证书,客户端跳过校验);仅当你用受信证书(如 Let's Encrypt)时才填证书域名 |
| `hysteria2.obfs_password` | 否 | 身份 | salamander 混淆密码,不填=不启用混淆 |

**凭据字段**(`vless.private_key/uuid/short_id`、`shadowsocks.password`、`hysteria2.password`):无需填写,
`make gen` 首次自动生成并写回清单,再次 gen 复用(幂等)。清单含凭据,请妥善保管(自动 0600 权限)。

**规则**:`name`/`address` 非空且不重复;至少配置一个通道;同服务器各通道端口不得冲突;
`server_name` 不能含空格或 `://`。

### 云安全组放行(每台服务器,控制台操作)

放行与你清单一致的实际端口(deploy.sh 只会放行服务器系统防火墙,云安全组需手动):

| 端口 | 协议 | 说明 |
|---|---|---|
| 443 | TCP | VLESS+Reality |
| 8388 | TCP + UDP | Shadowsocks(含 UDP 中继) |
| 8443 | UDP | Hysteria2(纯 QUIC,仅 UDP;TCP 放行冗余无妨) |

> 最易漏的一步:安全组不放行 = 客户端永远连不上。改端口时记得同步改安全组。

## 第 4 步:生成全部产物

```bash
make gen
```

输出 `dist/` 产物,含义见根 README 的产物结构。此时清单 `servers.json` 已回填全部凭据
(再次 `make gen` 不会翻新,已分发的客户端不受影响)。

## 第 5 步:部署到服务器

```bash
make deploy
```

逐台上传并远程执行,`deploy.sh` 在服务器上自动完成(幂等,可重复执行):

1. 系统防火墙放行端口(ufw / firewalld 自动识别)
2. 安装 Docker(未装时;官方脚本失败自动回退 apt 核心包)并启用开机自启
3. H2 自签证书生成(自签模式且证书缺失时,openssl EC 证书 10 年有效期)
4. 拉取 sing-box 锁版镜像(ghcr.io 失败自动回退 docker.io 备源)
5. 服务端配置语法校验(docker run sing-box check,失败即停不启动坏容器)
6. 启动容器并输出状态与日志(`--force-recreate` 保证配置变更生效)

任一台失败立即停止(错误信息含服务器名),修复后重跑 `make deploy` 即可,已成功的服务器不会重复拉取镜像。

## 第 6 步:验证

```bash
# 服务器上容器状态与日志(部署输出末尾已展示)
ssh root@服务器IP "docker compose -f /opt/sing-box/docker-compose.yml ps"
ssh root@服务器IP "docker compose -f /opt/sing-box/docker-compose.yml logs --tail=20"
# 应看到三行 inbound 监听日志:vless-in / ss-in / hy2-in 的 inbound started
```

客户端实测见 [docs/clients.md](docs/clients.md)。推荐的验证顺序:
先桌面 Clash Verge Rev 导入 `clash.yaml` 验证 443 通道,再手机扫 `links.txt` 的链接。

---

## 多服务器

清单数组里加一段即可,`make gen && make deploy` 一次处理全部;
客户端产物自动聚合全部服务器的全部通道(每服务器 3 个节点:name-vless / name-ss / name-h2)。

## 端口冲突与自定义

- 服务器上已有 nginx 占用 443:把 vless.port 改成其他端口(如 8443 被 h2 用则整体挪),同步改云安全组。
- 注意:Reality 的握手目标是伪装站点(www.apple.com)的 443,**与你的监听端口无关**。

## 受信证书(可选,替代 H2 自签)

自签证书对客户端完全可用(已跳过校验),仅当客户端所在网络对自签不友好时才需要受信证书:

1. 服务器上有域名并解析到服务器 IP,用 certbot 等签发证书
2. 清单填 `hysteria2.server_name` = 证书域名
3. `make gen` 后,把证书与私钥命名为 `cert.pem` / `key.pem` 放进 `dist/servers/<name>/`
4. `make deploy`(deploy.sh 检测到受信模式,不再自动生成自签,缺证书会报错引导)

## 升级 sing-box 版本

产物锁版镜像(compose 内 `ghcr.io/sagernet/sing-box:v1.14.0`):

1. 修改 `internal/server/render.go` 的 `ImageVersion` 常量(与镜像 tag 同步)
2. `make gen && make deploy`(镜像已存在则自动拉取新 tag 重建)

服务端配置模板与 sing-box schema 强相关,升级大版本后建议实测三通道连通性。
升级不涉及任何凭据(凭据在本地清单),已分发客户端不受影响。

## 备份与迁移

- **凭据唯一存放点**:`servers.json`(本地,0600)。备份它 = 备份全部节点身份。
- 服务器可随时销毁重建:重新 `make deploy` 同一份产物即恢复,客户端零感知。
- 换电脑:把项目目录(含 servers.json)拷走即可;或重新 git clone 后把 servers.json 拷入。

## 排障速查

| 现象 | 排查 |
|---|---|
| 客户端全部连不上 | 云安全组端口是否放行(第 3 步);服务器 `docker compose ps` 是否 Running;`docker compose logs` 是否 inbound started |
| deploy 卡在拉镜像 | ghcr.io 被墙时脚本自动回退 docker.io;仍失败则服务器手动 `docker pull docker.io/sagernet/sing-box:v1.14.0` |
| deploy 报"配置语法校验失败" | config.json 模板与 sing-box 版本不匹配(升级版本后常见),查看校验输出具体字段 |
| VLESS 连不上,SS/H2 正常 | 伪装站点被墙(换 `server_name`,如 `www.microsoft.com` / `www.amazon.com` 实测);443 端口被占用 |
| H2 连不上 | UDP 8443 是否放行(QUIC 只走 UDP,云安全组只放 TCP 必失败) |
| 换服务器 IP | 改清单 address 后 `make gen && make deploy`,客户端重新导入(节点地址变了,凭据未变) |
