# vpn:自建代理方案(从 0 到 1 完整闭环)

一套自建 VPN(代理)方案:sing-box 服务端 + 多客户端配置生成。
填写一份服务器清单,本地一条命令生成全部产物,再一条命令把服务端部署到你的海外服务器;
随后任意设备用主流客户端(sing-box 官方 / Hiddify / Clash Verge Rev / mihomo 等)导入配置即用。

> 定位:个人/小团队自用,极简无面板,清单驱动,服务端无状态。
> 不做什么:不做多用户面板、不做流量统计、不维护在线订阅服务。

---

## 快速开始(5 步)

```bash
# 1. 环境准备(Go 1.27+ / ssh / scp;macOS/Linux/Windows 均可)
make doctor

# 2. 填写服务器清单(复制示例后编辑 address 等字段,字段说明见下文)
cp example-servers.json servers.json
#   编辑 servers.json:服务器 IP/域名、SSH、端口、伪装站点

# 3. 本地生成全部产物(自动回填密钥并写回清单,幂等)
make gen

# 4. 一条命令部署到全部服务器(需已配置免密 SSH;内部自动装 Docker、放行防火墙、启动并验证)
make deploy

# 5. 客户端导入(dist/ 目录下的产物,详见 docs/clients.md)
#    - 桌面(Clash Verge Rev)   : 导入 dist/clash.yaml
#    - 手机(iOS/Android)        : 扫码/粘贴 dist/links.txt 里的链接
#    - sing-box 官方客户端      : 导入 dist/sing-box.json
```

完整部署指导(买服务器、SSH 密钥、云安全组放行等从 0 到 1)见 [docs/deployment.md](docs/deployment.md)。

---

## 架构总览

```mermaid
flowchart LR
    subgraph 本地(你的一台电脑)
        A[servers.json<br/>服务器清单] -->|make gen| B[配置生成器 cmd/vpn]
        B --> C[dist/servers/&lt;name&gt;/<br/>config.json + compose + deploy.sh]
        B --> D[dist/<br/>links.txt / sub.txt<br/>clash.yaml / sing-box.json]
        B -->|凭据回填,幂等| A
    end
    subgraph 海外服务器(每台)
        C -->|make deploy: ssh + scp| E[/opt/sing-box/]
        E -->|docker compose up| F[(sing-box 容器<br/>VLESS+Reality :443<br/>Shadowsocks :8388<br/>Hysteria2 :8443)]
    end
    subgraph 客户端设备(全平台)
        D -->|导入| G[Clash Verge Rev / mihomo]
        D -->|导入| H[sing-box 官方 SFI/SFA/CLI]
        D -->|导入| I[Hiddify / 移动端 app]
        G & H & I --> F
    end
```

设计要点(完整分析见 [docs/architecture.md](docs/architecture.md)):

- **单一事实来源**:所有密钥/UUID/密码只在本地清单 `servers.json` 生成并回填,
  服务端配置与客户端产物从同一份清单渲染,两端凭据不可能漂移。
- **服务端无状态**:服务器只消费静态 `config.json`,不保存任何用户状态;
  删机重建后重新 `make deploy` 同一份产物即可,已分发客户端不受影响。
- **三协议通道**,覆盖主流客户端生态的导入面:
  - VLESS+Reality(TCP 443):抗封锁主力,Clash 系/sing-box 系/Hiddify 均支持
  - Shadowsocks(TCP+UDP 8388):最老牌最广兼容,任何客户端保底可用
  - Hysteria2(UDP 8443):QUIC 逃生/提速通道,自签证书零成本
- **单文件部署产物**:每台服务器一个目录,`deploy.sh` 幂等可重跑
  (自动装 Docker、放行防火墙、自签证书、语法校验、启动并验证)。

---

## 目录结构

```text
vpn/
├── cmd/vpn/               CLI 入口(gen / deploy / doctor)
├── internal/
│   ├── conf/              清单模型:校验、凭据回填、原子保存、节点推导
│   ├── server/            服务端产物生成(sing-box 配置 + 编排/部署脚本)
│   ├── client/            客户端产物生成(分享链接/订阅/Clash/sing-box)
│   └── deploy/            远程部署执行(ssh/scp 编排)
├── docs/                  文档地图(见下)
├── example-servers.json   清单示例(复制为 servers.json 后填写)
├── servers.json           实际清单(自动回填凭据,勿提交到 git)
├── Makefile               命令固化(doctor/gen/deploy/check)
└── AGENTS.md              项目 Agent 协作规范
```

产物结构(`dist/`,生成后见 `make gen` 输出):

```text
dist/
├── servers/<name>/        每台服务器的部署产物(scp 到服务器一键启动)
│   ├── config.json        sing-box 服务端配置(0600,含凭据)
│   ├── docker-compose.yml 容器编排(锁版镜像,host 网络)
│   ├── deploy.sh          一键部署脚本(幂等)
│   └── cert.sh            H2 自签证书脚本(仅自签模式服务器)
├── links.txt              全部节点分享链接(每行一个,剪贴板/扫码导入)
├── sub.txt                通用订阅(base64 全链接,机场标准格式)
├── clash.yaml              Clash 系订阅(节点组 + 国内直连分流规则)
└── sing-box.json           sing-box 官方客户端完整配置
```

---

## 文档地图

| 文档 | 内容 |
|---|---|
| [docs/deployment.md](docs/deployment.md) | 服务端部署从 0 到 1:本地环境、买服务器、SSH 密钥、清单填写、云安全组、生成与部署、验证与排障 |
| [docs/clients.md](docs/clients.md) | 客户端对接总览:分发物说明、各客户端导入路径、能力与版本要求 |
| [docs/clients/sing-box.md](docs/clients/sing-box.md) | sing-box 官方客户端(SFI/SFA/CLI)对接指南 |
| [docs/clients/hiddify.md](docs/clients/hiddify.md) | Hiddify(桌面/移动全平台)对接指南 |
| [docs/clients/clash-verge.md](docs/clients/clash-verge.md) | Clash Verge Rev(Windows/macOS/Linux)对接指南 |
| [docs/clients/mihomo.md](docs/clients/mihomo.md) | mihomo 内核/OpenClash(含网关盒子)对接指南 |
| [docs/platforms.md](docs/platforms.md) | 平台兼容矩阵:Windows/macOS/Linux/Android/iOS/网关盒子 × 客户端 |
| [docs/architecture.md](docs/architecture.md) | 架构设计:模块职责、数据流、协议选型依据、安全模型、扩展方向 |
| [docs/faq.md](docs/faq.md) | 常见问题:连接失败排查、证书、端口冲突、升级、备份迁移 |

## 开发与验证

```bash
make check        # build + vet + test + fmt 全量验证(改动后必跑)
make test-race    # 竞态检测(可选)
make clean        # 清理 dist/ 产物(servers.json 凭据保留)
```

代码与文档规范见 [AGENTS.md](AGENTS.md);项目采用简体中文注释与文档。
