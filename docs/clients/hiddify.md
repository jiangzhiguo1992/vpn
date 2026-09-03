# Hiddify 对接

Hiddify(sing-box 内核 fork)覆盖 Windows / macOS / Linux / Android / iOS 全平台,
自带成熟的分流域名规则与广告拦截,开箱即用,是移动端最省心的选择。
双通道(vless+Reality / hysteria2)全部支持。

## 产物

Hiddify 用"分享链接"或"订阅"添加节点,不直接吃 clash/sing-box 配置:

- `dist/links.txt`:逐行复制或扫码
- `dist/sub.txt`:订阅内容(支持把任意静态 URL 作为订阅源)

## 导入方式

### 方式一:单链接导入(最简)

1. 打开 `dist/links.txt`,任选一行(推荐 vless 开头行:主力通道)
2. Hiddify 首页点 "+"(添加),选"手动添加",粘贴链接;或直接扫节点二维码
3. 重复添加多个节点后,点连接即用

### 方式二:订阅导入(多节点一次到位)

Hiddify 需要订阅 URL。`dist/sub.txt` 是标准 base64 订阅内容,两种用法:

1. **本地静态托管**(可选):把 `sub.txt` 内容传到任意可达的 URL(如 GitHub Gist、
   自己的静态站点,或云对象存储),在 Hiddify 添加"订阅链接"填该 URL
2. **临时内网托管**:本机起一个一次性静态服务再添加订阅,导入后即可断开:

```bash
# 本机临时托管(局域网内设备导入用)
cd dist && python3 -m http.server 8000
# Hiddify 添加订阅: http://<本机IP>:8000/sub.txt
```

> 极简提示:个人自用节点少,方式一逐个添加已够用;订阅 URL 主要面向需要
> 多设备同步与后续换服务器免重配的场景。

## 平台要点

| 平台 | 说明 |
|---|---|
| Android | Play/官网下载,连接页选节点,自带按 app 分流 |
| iOS | 区外 Apple ID 下载;添加方式同上 |
| Windows/macOS/Linux | 桌面版同理,系统代理与 TUN 模式可切 |

## 常见问题

- **连接失败先看哪**:Hiddify 页面顶部显示连接状态,点开节点详情看错误:
  timeout 类查云安全组端口放行;Reality 握手失败查伪装站点。
- **国内访问与分流**:Hiddify 默认规则已处理国内直连,无需在节点上做任何配置。
- **只用某通道**:订阅/链接添加的是全部节点,在节点列表挑需要的即可,
  其余节点不连接不产生流量。
