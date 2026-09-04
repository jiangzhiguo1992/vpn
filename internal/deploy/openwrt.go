// OpenWrt 网关盒子(裸 sing-box)一键部署编排:目标为单台 OpenWrt 网关,
// 由用户以 <user>@<host> 直接指定(网关不入服务器清单,故不依赖 conf)。
// 流程 = 检查本地产物 → scp 上传到 /tmp/sing-box-openwrt.json → ssh
// 执行 OpenWrtScript(远端一步到位,见常量注释)。
//
// 设计决策:
//   - 复用 baseArgs/sshHost/runCmd:端口形态(ssh -p / scp -P)、IPv6
//     方括号、超时保护、非交互三件套与 mock 注入点均与清单部署(deploy.go)
//     完全一致,两套部署共用一套 SSH 参数口径
//   - 命令构造纯函数化(openWrtSSHCommand/openWrtSCPCommand 返回 exec
//     args,不含 argv0)与 host 解析(parseOpenWrtHost)均为纯函数,便于
//     单测断言参数形态,不实际连接
//   - 对外入口只暴露最简签名 RunOpenWrt(hostArg, port, configPath):
//     hostArg 含可选 user@,内部解析为 openWrtTarget 再执行,减少调用方
//     出错面;非法目标(多 @/空 host)在解析期即返回 error,不发任何
//     远程动作
//   - 错误即停:scp/ssh 任一步失败立即返回,错误含目标地址;OpenWrtScript
//     自身幂等且每步失败即退
//   - 回滚安全次序(OpenWrtScript 内):UCI/init 集成与 sing-box check
//     先行(不触碰现网配置)→ 备份齐全(config.json/dhcp/UCI)→ 落位 →
//     UCI 启用 → 启动 → 最后才 dnsmasq DNS 让位(启动成功后才切)
//
// 职责边界:仅编排与命令构造;渲染产物在 internal/client(OpenWrt 变体
// sing-box-openwrt.json),写盘在 cmd/vpn gen;远端动作全部收敛在
// OpenWrtScript 常量内,便于审查与单测。
package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// OpenWrtScript 是 OpenWrt 网关盒子一键部署脚本(单次 ssh 执行,set -eu +
// busybox ash 兼容,幂等可重跑)。活动域仅 /tmp 与 /etc/sing-box。
//
// 步骤与回滚安全次序(echo 编号一一对应):
//  1. 前置检查:/tmp/sing-box-openwrt.json 存在(scp 上传失败即中止)并
//     chmod 600(文件含全量节点凭据,收紧读取权限)
//  2. 无 sing-box 自动安装(官方源:apk 新 / opkg 传统,含 auto_route
//     依赖 ip-full;两者皆无则提示手动安装;安装后统一复检——包记录残留
//     会跳过补装,仍缺二进制时提示 force-reinstall/apk fix)
//  3. UCI sing-box.main + init 脚本存在性检查(官方源/apk/opkg 安装后
//     自带;刚装完仍缺失则多为第三方/手动形态,提示改用官方源或第三方
//     源 ipk)——置于安装之后:全新盒子(UCI/init/二进制皆无)也能先装好
//     二进制再进入本检查,而非死在集成检查上
//  4. sing-box check -c /tmp/sing-box-openwrt.json 语法校验(不触碰现网,
//     回滚安全第一道闸;失败先打版本首行再报错)
//  5. 备份现网(config.json → .bak、/etc/config/dhcp → .bak、UCI
//     /etc/config/sing-box → .bak;.bak 已存在则保留首个并提示,幂等)
//  6. 落位 mkdir/cp/chmod 600,随即删除 /tmp 上传件(不留凭据残本)
//  7. UCI 启用 + user=root(root 运行是 TUN 前提,官方 init 注释明确)
//  8. 启动(logger 打时间戳唯一锚 anchor=deploy-restart-begin-$(date +%s)
//     后 /etc/init.d/sing-box restart;锚供验证段区分本次启动日志)
//  9. dnsmasq DNS 让位(仅禁 DNS 保留 DHCP;先判 section 与进程存活,
//     停用的 dnsmasq 不被意外拉起)
//     最后:验证 sing-box 进程(轮询最长 30s;命中后 5s 持续存活复检——
//     首启节点不可达时规则集下载数秒后 FATAL 退出,单次命中会误报;
//     复检通过后再做锚后 logread FATAL 双检,logd 环形缓冲不清空,
//     旧 FATAL 残留不得误报)
const OpenWrtScript = `set -eu

# ===== OpenWrt 网关盒子 sing-box 一键部署(幂等,可重复执行) =====
# 活动域:/tmp 与 /etc/sing-box;先校验后落位,备份齐全,任一步失败即停。

echo "==== 1/9 前置检查:上传的配置 ===="
if [ ! -f /tmp/sing-box-openwrt.json ]; then
  echo "缺少 /tmp/sing-box-openwrt.json(scp 上传失败?),中止" >&2
  exit 1
fi
# 该文件含全量节点凭据(协议口令/密钥),收紧为仅属主可读
chmod 600 /tmp/sing-box-openwrt.json

echo "==== 2/9 检查 sing-box,缺失则自动安装(官方源) ===="
if ! command -v sing-box >/dev/null 2>&1; then
  if command -v apk >/dev/null 2>&1; then
    echo "用 apk 安装 sing-box + ip-full(TUN auto_route 需 ip-full)"
    apk update
    apk add sing-box ip-full
  elif command -v opkg >/dev/null 2>&1; then
    echo "用 opkg 安装 sing-box + ip-full"
    opkg update
    opkg install sing-box ip-full
  else
    echo "无 apk/opkg 包管理器,请手动安装 sing-box + ip-full 后重跑" >&2
    exit 1
  fi
else
  echo "sing-box 已存在,跳过安装"
fi
# 装后复检:opkg/apk 在包记录已存在时会跳过补装(文件残缺不补),须强制重装
if ! command -v sing-box >/dev/null 2>&1; then
  echo "安装后 sing-box 仍不可用(包记录已存在时会跳过补装):请手动执行 opkg install --force-reinstall sing-box 或 apk fix sing-box 后重跑" >&2
  exit 1
fi

echo "==== 3/9 UCI/init 集成检查(官方源包自带) ===="
if ! uci get sing-box.main >/dev/null 2>&1 || [ ! -x /etc/init.d/sing-box ]; then
  echo "缺少 UCI 配置 sing-box.main 或 init 脚本(官方源/apk/opkg 安装后自带 /etc/config/sing-box 与 /etc/init.d/sing-box;若刚安装完仍缺失,可能是第三方/手动形态),请改用官方源或第三方源 ipk 后重跑" >&2
  exit 1
fi

echo "==== 4/9 sing-box check 语法校验(回滚安全第一道闸,不动现网) ===="
if ! sing-box check -c /tmp/sing-box-openwrt.json; then
  echo "--- sing-box version 首行(供排障) ---"
  sing-box version 2>&1 | head -n 1 || true
  echo "sing-box check 失败:内核过旧或语法不兼容,见 docs/openwrt.md 升级内核" >&2
  exit 1
fi

echo "==== 5/9 备份现网(供回滚;.bak 已存在则保留首个备份) ===="
if [ -f /etc/sing-box/config.json ] && [ ! -f /etc/sing-box/config.json.bak ]; then
  cp /etc/sing-box/config.json /etc/sing-box/config.json.bak
  echo "已备份 /etc/sing-box/config.json → config.json.bak"
elif [ -f /etc/sing-box/config.json ]; then
  echo "config.json.bak 已存在:保留首次部署前备份(不覆盖);如需以当前配置为新基准,请先自行备份"
fi
if [ -f /etc/config/dhcp ] && [ ! -f /etc/config/dhcp.bak ]; then
  cp /etc/config/dhcp /etc/config/dhcp.bak
  echo "已备份 /etc/config/dhcp → dhcp.bak(供 dnsmasq 回滚)"
elif [ -f /etc/config/dhcp ]; then
  echo "dhcp.bak 已存在:保留首次部署前备份(不覆盖);如需以当前配置为新基准,请先自行备份"
fi
if [ -f /etc/config/sing-box ] && [ ! -f /etc/config/sing-box.bak ]; then
  cp /etc/config/sing-box /etc/config/sing-box.bak
  echo "已备份 /etc/config/sing-box → sing-box.bak(UCI 回滚)"
elif [ -f /etc/config/sing-box ]; then
  echo "sing-box.bak 已存在:保留首次部署前备份(不覆盖);如需以当前配置为新基准,请先自行备份"
fi

echo "==== 6/9 落位配置 ===="
mkdir -p /etc/sing-box
cp /tmp/sing-box-openwrt.json /etc/sing-box/config.json
chmod 600 /etc/sing-box/config.json
# 落位后删除 /tmp 上传件(含全量节点凭据,不留残本;运行期不再需要)
rm -f /tmp/sing-box-openwrt.json

echo "==== 7/9 UCI 启用 + root 运行 ===="
uci set sing-box.main.enabled='1'
uci set sing-box.main.user='root'
uci commit sing-box
/etc/init.d/sing-box enable

echo "==== 8/9 启动 sing-box ===="
# 日志锚(时间戳唯一):供验证段区分本次启动日志。logd 环形缓冲不清空,
# 重跑部署时窗口内会残留历史锚与旧 FATAL——固定锚文本会让验证段命中
# 最早的历史锚,把旧 FATAL 误判为本次启动(容器演练实测);时间戳锚保证
# 只认本次 restart 之后的日志
anchor="deploy-restart-begin-$(date +%s)"
logger -t sing-box "$anchor"
/etc/init.d/sing-box restart

echo "==== 9/9 dnsmasq DNS 让位(仅禁 DNS,DHCP 保留;成功启动后才切) ===="
# 无 dnsmasq section(罕见,如仅跑 sing-box 的盒子)则无让位可做
if ! uci get dhcp.@dnsmasq[0] >/dev/null 2>&1; then
  echo "未发现 dnsmasq section dhcp.@dnsmasq[0],跳过 DNS 让位"
else
  old_port=""
  if uci get dhcp.@dnsmasq[0].port >/dev/null 2>&1; then
    old_port="$(uci get dhcp.@dnsmasq[0].port)"
  fi
  if [ "$old_port" != "0" ]; then
    uci set dhcp.@dnsmasq[0].port='0'
    uci commit dhcp
    # 仅当 dnsmasq 存活才 restart:被停用的 dnsmasq 不应被意外拉起
    # (端口配置已改,待其下次启动即生效)
    if pgrep -x dnsmasq >/dev/null 2>&1; then
      /etc/init.d/dnsmasq restart
    else
      echo "dnsmasq 未在运行,跳过 restart(端口配置已改,待下次启动生效)"
    fi
    if [ -n "$old_port" ]; then
      echo "dnsmasq DNS 已让位(原端口 $old_port;恢复:uci set dhcp.@dnsmasq[0].port='$old_port' && uci commit dhcp && /etc/init.d/dnsmasq restart)"
    else
      echo "dnsmasq DNS 已让位(原端口未显式配置,缺省 53)"
    fi
  else
    echo "dnsmasq DNS 端口已是 0,无需让位"
  fi
fi

echo "==== 验证 sing-box 进程(轮询最长 30 秒) ===="
# restart 后进程拉起需要时间:每 2 秒轮询一次(共 15 次)首次命中即退出
# 循环;while/if 条件中的命令失败不触发 set -e,整体 set -eu 语义不变
i=0; while [ $i -lt 15 ]; do pgrep -f 'sing-box run' >/dev/null 2>&1 && break; sleep 2; i=$((i+1)); done
if ! pgrep -f 'sing-box run' >/dev/null 2>&1; then
  echo "sing-box 未在运行,日志:logread -e sing-box(若为首启且规则集需在线下载,启动可能超过 30 秒,稍后重跑一次即可)" >&2
  exit 1
fi
# 稳定窗口:进程命中 ≠ 持续存活。首启若节点不可达,规则集下载会在数秒后
# FATAL 退出(容器演练实测:启动后约 5 秒 FATAL),单次命中会误报成功;
# 等待 5 秒复检确认持续存活(正常启动的进程不受影响,弱盒子反而更稳)
sleep 5
if ! pgrep -f 'sing-box run' >/dev/null 2>&1; then
  echo "sing-box 启动后退出(多为规则集首启下载失败或 tun 创建失败),日志:logread -e sing-box;若为首启下载超时,稍后重跑一次即可" >&2
  exit 1
fi
# 双检:进程虽在但本次启动即 FATAL(规则集下载失败或 tun 创建失败)同样判
# 失败。只认本次时间戳锚行之后出现的 fatal(awk -v 注入唯一锚,历史锚与
# 旧 FATAL 均不误报;日志为大写 "FATAL",正则经 tolower 归一——两处均为
# 容器演练实测发现)
fatal_found="$(logread -e sing-box 2>/dev/null | tail -n 200 | awk -v a="$anchor" 'index($0,a){f=1} f && tolower($0) ~ /fatal/{print "1"; exit}')"
if [ -n "$fatal_found" ]; then
  echo "本次启动检出 sing-box FATAL(规则集首启下载失败或 tun 创建失败),见 logread -e sing-box" >&2
  exit 1
fi
echo "✓ OpenWrt 网关 sing-box 部署完成(TUN 全接管,DNS 已让位)"
echo "恢复与排障指引见 docs/openwrt.md"
`

// openWrtTarget 是 OpenWrt 网关的 SSH 目标(user 缺省 root;host 为
// 剥离方括号后的裸地址,组装命令时经 sshHost 加回 IPv6 方括号)。
type openWrtTarget struct {
	user string
	host string
	port int
}

// target 组合 <user>@<host>(IPv6 加回方括号,防 scp 按冒号截断 host)。
func (t openWrtTarget) target() string {
	return t.user + "@" + sshHost(t.host)
}

// parseOpenWrtHost 解析 "<user>@<host>" 形式的 OpenWrt SSH 目标。
//
// 返回 error 的场景(均在发起任何远程动作前拦截):多个 @(user 段不能含
// @)、剥括号后 host 为空(空串/纯空白/裸 @)。user 为空(如 "@host")回退
// root;host 若带 [v6] 方括号则剥离(组装时 sshHost 自动加回)。port 为
// 独立参数(不解析进 host 串)。
func parseOpenWrtHost(hostArg string, port int) (openWrtTarget, error) {
	hostArg = strings.TrimSpace(hostArg)
	if strings.Count(hostArg, "@") > 1 {
		return openWrtTarget{}, fmt.Errorf("deploy.parseOpenWrtHost: 目标 %q 含多个 @,应为 <user@host>", hostArg)
	}
	user, host := "root", hostArg
	if i := strings.Index(hostArg, "@"); i >= 0 {
		user, host = hostArg[:i], hostArg[i+1:]
	}
	if user == "" {
		user = "root"
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if host == "" {
		return openWrtTarget{}, fmt.Errorf("deploy.parseOpenWrtHost: 目标 %q 缺 host,应为 <user@host>", hostArg)
	}
	return openWrtTarget{user: user, host: host, port: port}, nil
}

// openWrtSSHCommand 构造对 OpenWrt 网关执行远程命令的 ssh 参数(不含
// argv0)。remoteCmd 为整段远程 shell 文本(多行脚本作单个参数直传,
// 本地不经 shell)。
func openWrtSSHCommand(t openWrtTarget, remoteCmd string) []string {
	args := baseArgs(t.port, "-p")
	args = append(args, t.target(), remoteCmd)
	return args
}

// openWrtSCPCommand 构造向 OpenWrt 网关上传配置的 scp 参数(不含
// argv0)。远端落点固定 /tmp/sing-box-openwrt.json(不依赖源文件名,
// 与 OpenWrtScript 取用名一致)。
func openWrtSCPCommand(t openWrtTarget, srcFiles ...string) []string {
	args := baseArgs(t.port, "-P")
	args = append(args, srcFiles...)
	args = append(args, t.target()+":/tmp/sing-box-openwrt.json")
	return args
}

// RunOpenWrt 对单台 OpenWrt 网关盒子一键部署 sing-box(对外入口)。
//
// 使用示例:
//
//	err := deploy.RunOpenWrt("root@192.168.1.1", 22, "dist/sing-box-openwrt.json")
//
// 调用说明:hostArg 为 "<user>@<host>"(user 缺省 root,IPv6 可带方括号;
// 多 @ 或缺 host 时报错);port 为 SSH 端口(22 默认);configPath 为本地
// 产物(cmd/vpn gen 生成,文件名须为 sing-box-openwrt.json,远端脚本按此
// 名取用)。返回:成功 nil,中途失败返回含目标地址的包装错误。
func RunOpenWrt(hostArg string, port int, configPath string) error {
	t, err := parseOpenWrtHost(hostArg, port)
	if err != nil {
		return err
	}
	if err := runOpenWrt(t, configPath); err != nil {
		return err
	}
	fmt.Printf("✓ OpenWrt 网关 %s 部署完成\n", t.target())
	fmt.Println("后续:局域网设备以 网关 为 DNS/网关即走 sing-box;恢复与排障见 docs/openwrt.md")
	return nil
}

// runOpenWrt 执行编排:检查产物 → scp 上传 /tmp → ssh 执行 OpenWrtScript。
// 任一步失败立即返回(错误含目标地址),OpenWrtScript 幂等,修复后重跑安全。
func runOpenWrt(t openWrtTarget, configPath string) error {
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("deploy.RunOpenWrt: %s 产物不存在(先 make gen): %s", t.target(), configPath)
		}
		return fmt.Errorf("deploy.RunOpenWrt: %s 读取产物失败: %s: %w", t.target(), configPath, err)
	}
	fmt.Printf("==> 部署 OpenWrt 网关 %s...\n", t.target())
	// 1. 上传配置到 /tmp(远端脚本活动域,不触碰现网)
	if err := runCmd("scp", openWrtSCPCommand(t, configPath)); err != nil {
		return fmt.Errorf("deploy.RunOpenWrt: %s 上传配置失败: %w(若报 subsystem request failed:OpenWrt 盒子需先 opkg install openssh-sftp-server,OpenSSH 9+ scp 默认走 SFTP)", t.target(), err)
	}
	// 2. 远端一键脚本(集成检查/安装/校验/备份/落位/UCI/启动/DNS 让位/验证)
	if err := runCmd("ssh", openWrtSSHCommand(t, OpenWrtScript)); err != nil {
		return fmt.Errorf("deploy.RunOpenWrt: %s 远程部署失败: %w", t.target(), err)
	}
	return nil
}
