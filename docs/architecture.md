# 架构设计

## 1 定位与边界

自建代理方案的"配置生成 + 服务端部署"闭环,覆盖场景:个人/小团队,自有海外服务器,
全平台主流客户端对接。**明确不做**:多用户管理、流量统计/计费、在线订阅托管服务、
自研客户端内核(客户端全部用现成主流实现)。

```mermaid
flowchart TB
    subgraph 清单层(单一事实来源)
        JSON[servers.json<br/>身份字段=用户填,凭据字段=回填]
    end
    subgraph 生成层(本地,零网络)
        GEN[cmd/vpn gen] --> CONF[internal/conf<br/>校验/回填/IO]
        GEN --> SVR[internal/server<br/>服务端产物]
        GEN --> CLI[internal/client<br/>客户端产物]
    end
    subgraph 部署层
        DEP[cmd/vpn deploy] --> EXE[internal/deploy<br/>ssh/scp 编排]
    end
    subgraph 运行时(海外服务器)
        DOCKER[docker compose<br/>sing-box 锁版镜像<br/>host 网络]
    end
    CONF --> SVR
    CONF --> CLI
    SVR --> DEP --> DOCKER
    CLI --> USERS[客户端设备]
```

## 2 模块职责

| 包 | 职责 | 关键文件 |
|---|---|---|
| `cmd/vpn` | CLI 编排:gen/deploy/doctor;产物写盘与权限 | main.go |
| `internal/conf` | 清单模型、校验、凭据回填、原子保存、节点反推 | model/validate/io/node/keys.go |
| `internal/server` | 服务端 sing-box 配置渲染 + compose/deploy.sh/cert.sh | render.go/artifact.go |
| `internal/client` | 分享链接、base64 订阅、clash.yaml、sing-box.json 渲染 | link/singbox/clash/sub.go |
| `internal/deploy` | ssh/scp 逐台上传与远程执行 | deploy.go |

依赖方向:`cmd → {server, client, deploy, conf}`、`{server, client, deploy} → conf`。
conf 是唯一被多方引用的包,保证清单(凭据)是全部产物的单一来源。

## 3 关键设计决策(含取舍记录)

### 3.1 为什么服务端用 sing-box 官方镜像

- 单容器支持本方案全部三协议(VLESS+Reality/SS/Hysteria2),一个进程一个配置,无面板
- 与主流客户端生态同源(sing-box 官方客户端、Hiddify 基于 sing-box;Clash 系对三协议支持完整)
- 相对 xray 面板方案(3x-ui 等):无数据库/无 Web 服务面,攻击面小、迁移简单、无状态
- 镜像锁 tag(`v1.14.0`),升级可控(见 deployment.md)

### 3.2 为什么三协议通道(Reality + SS + Hy2)

兼容矩阵推导(客户端生态 × 协议支持,细节见 [platforms.md](platforms.md)):

| 通道 | 定位 | 兼容面 |
|---|---|---|
| VLESS+Reality | 主力(抗封锁,无证书) | 2024 年后的 Clash 系/Hiddify/sing-box/iOS 新版 app |
| Shadowsocks | 保底(最老牌) | 一切客户端,含不支持 Reality/Hy2 的旧 app |
| 场景 | 加速与逃生(QUIC 抗丢包) | Clash 系 v1.18+/sing-box/Hiddify |

三通道即可覆盖全部点名客户端与平台;VMess+Trojan+WS 等需域名证书,
个人自用收益低,不做(清单模型留了扩展位,见 5)。

### 3.3 为什么"本地生成 + 静态产物"而不是面板/在线订阅

- 用户侧诉求是"让用户填服务器信息 → 生成客户端配置",清单文件即填写载体,生成即分发
- 静态产物零运行时依赖(不需要 Web 服务、数据库、后台任务),服务器可随时销毁重建
- 无在线订阅 = 无过期/鉴权/托管问题;节点变更重新分发一次即可
- 若未来需要在线订阅,`sub.txt` 已是标准格式,托管到任意静态 URL 即完成(见 clients/hiddify.md)

### 3.4 为什么生成器零第三方依赖(纯标准库)

- 服务端配置与客户端配置均为固定结构文本,模板渲染即可,不需要引入 sing-box option 库
  (旧方案依赖 sing-box Go 库:重依赖、构建标签、分钟级编译;本方案构建秒级)
- 正确性保障从"类型系统"转为"模板 + 渲染后语法校验(json.Valid)+ 一致性单测",
  并部署时 docker run sing-box check 在服务器上做最终 schema 校验(见 deploy.sh)
- 值统一 JSON 转义后入模板,防注入;模板缺 key 静默输出 `<no value>` 已做拦截

### 3.5 为什么凭据只在本地生成回填(服务端无状态)

- 全部随机凭据(Reality 密钥对/UUID/ShortID/SS/Hy2 密码)在 `make gen` 时生成并写回清单
- 服务器只消费静态 config.json,不持有任何生成逻辑 → 服务器可任意重建,
  凭据不变则已分发客户端全部免更新
- 清单保存为 0600 原子写,防凭据泄露与写坏;加载为严格模式,字段拼错即时报错
- **防漂移**:服务端 config 与客户端产物(链接/clash/sing-box)全部从同一清单渲染,
  单测黄金断言交叉校验(见各包 *_test.go)

### 3.6 分流规则的取舍(两种客户端形态)

| 产物 | 分流策略 | 原因 |
|---|---|---|
| clash.yaml | GEOSITE,cn / GEOIP,CN 国内直连 | Clash 系客户端(Verge/OpenClash)自带 geodata,开箱可用 |
| sing-box.json | 全局代理 + 内网直连 | 官方 app 不依赖外置规则文件,导入即用;需要国内分流的按文档追加(见 clients/sing-box.md) |

## 4 安全模型

- 本地:清单/全部含凭据产物 0600(显式 Chmod,防旧权限残留);凭据不进入 git(servers.json 已忽略)
- 传输:客户端与服务端之间凭据即认证(Reality 公钥体系 / SS 密码 / Hy2 密码),
  Reality 流量伪装为 TLS 到公开站点
- 服务器:仅暴露三个监听端口;Docker 容器只读挂载配置;无面板无多余服务面
- SSH 部署链路:非交互(BatchMode)、首次指纹 accept-new、超时保护、错误即停
- H2 证书:默认自签(insecure 跳过校验);要受信证书时清单声明 server_name 并自行放置
- shell 注入:产物脚本内嵌值(CN/端口)经校验与 shell 单引号转义双重防线

## 5 扩展方向(未实现,需要时再加)

- **新增协议通道**:模型扩展点已具备(conf 通道结构 + server 片段 + client 渲染三分支),
  如 VMess+WS+TLS(需域名证书)可照三通道同构追加,并在链接/clash/sing-box/订阅四处同步
- **在线订阅更新**:托管 `sub.txt` 到任意静态 URL(无需本仓库代码)
- **多用户/分享**:清单结构按用户拆多份分别 gen 即可,不做面板
- **IPv6 出口优化**:服务器启用 v6 后,客户端 DNS 策略在 sing-box.json 中调整

## 6 验证体系

| 层 | 手段 |
|---|---|
| 单元 | 校验/回填幂等/链接解析/渲染结构断言(表驱动,边界与异常覆盖) |
| 一致性 | 跨产物黄金断言:同一凭据在服务端配置与各客户端产物中一致(防漂移) |
| 端到端 | cmd 冒烟:临时清单 → 全产物 → 幂等断言 |
| 本地 | `go build ./...` + `go vet ./...` + `go test ./...` + `gofmt`(make check) |
| 真机 | `make deploy` 后服务器侧 sing-box check + 容器日志 inbound started + 客户端实测连通 |
