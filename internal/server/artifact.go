// Package server 服务端产物写盘:config.json / docker-compose.yml /
// deploy.sh / cert.sh,含编排与部署脚本模板。
//
// 设计决策:
//   - docker-compose 用 network_mode: host(无端口映射,容器直出;
//     443/8443 端口由 sing-box 进程直接占用),镜像与版本常量
//     ImageRef 同源(见 render.go)
//   - 证书两态(与 conf.H2Config.ServerName 语义一致):空 = IP+自签,
//     生成 cert.sh 且 deploy.sh 自动执行;非空 = 受信证书,deploy.sh
//     校验 cert.pem/key.pem 存在,缺失时报错引导(不生成自签覆盖用户
//     受信证书意图)
//   - deploy.sh 幂等可重跑:防火墙放行、Docker 安装兜底、Compose v2
//     插件补装(存量 docker 仅 docker-compose v1 时 apt 装
//     docker-compose-plugin,docker compose 子命令才能用)、开机自启
//     兜底、镜像 ghcr 拉取失败回退 docker.io 并 retag、up 前 docker
//     run check 语法校验(H2 通道挂载证书,校验容器与运行容器挂载语义
//     一致,防 check 误报 /etc/sing/cert.pem 缺失)、up --force-recreate
//     (配置变化不触发 compose 重建)
//   - 产物文件权限:config.json 0600(含凭据);脚本 0755;compose 0644;
//     全部显式 Chmod(WriteFile 的 mode 仅创建时生效,重复 gen 会残留
//     旧权限)
//   - 清单从「有 H2」改「无 H2」后清理残留 cert.sh(scp 一并上传会
//     执行过时脚本生成无用证书)
//
// 职责边界:产物写盘与权限;内容渲染在 render.go;
// 清单模型在 internal/conf。
//
// 使用示例:
//
//	err := server.WriteArtifacts("dist/servers/hk-01", srv)
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vpn/internal/conf"
)

// remoteConfigPath 是容器内配置路径(compose 挂载与 deploy.sh 校验命令
// 一致,配置挂载语义见 composeYAML)。
const remoteConfigPath = "/etc/sing-box/config.json"

// WriteArtifacts 生成单台服务器的全部服务端产物到 dir。
//
// 调用说明:dir 不存在时自动创建;conf.Server 须已通过 Validate +
// Backfill。参数 dir:产物目录(如 dist/servers/hk-01);s:清单条目。
// 返回:任一文件写失败时返回含路径的包装错误。
func WriteArtifacts(dir string, s *conf.Server) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("server.WriteArtifacts: 创建目录 %s: %w", dir, err)
	}
	data, err := RenderConfig(s)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "config.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeYAML(s)), 0o644); err != nil {
		return err
	}
	sh, err := deployScript(s)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "deploy.sh"), []byte(sh), 0o755); err != nil {
		return err
	}
	// cert.sh 仅自签模式生成(受信模式用户自行放置证书,脚本无意义)
	if s.Hysteria2 != nil && s.Hysteria2.ServerName == "" {
		if err := writeFile(filepath.Join(dir, "cert.sh"), []byte(certScript(s)), 0o755); err != nil {
			return err
		}
	} else {
		// 清理残留(清单从「自签」改「受信/无 H2」后旧 cert.sh 不再执行);
		// ENOENT 属预期不报错
		if err := os.Remove(filepath.Join(dir, "cert.sh")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("server.WriteArtifacts: 删除 %s/cert.sh: %w", dir, err)
		}
	}
	return nil
}

// writeFile 以指定权限写文件(显式 Chmod,防旧权限残留)。
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("server.WriteArtifacts: 写 %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("server.WriteArtifacts: 设置 %s 权限 %o: %w", path, mode, err)
	}
	return nil
}

// composeYAML 生成 docker-compose 编排(host 网络;H2 通道追加证书挂载)。
func composeYAML(s *conf.Server) string {
	var volumes string
	if s.Hysteria2 != nil {
		volumes = `
      - ./cert.pem:/etc/sing/cert.pem:ro
      - ./key.pem:/etc/sing/key.pem:ro`
	}
	return fmt.Sprintf(`services:
  sing-box:
    image: %s
    container_name: sing-box
    network_mode: host
    restart: unless-stopped
    volumes:
      - ./config.json:%s:ro%s
    command: run -c %s
`, ImageRef, remoteConfigPath, volumes, remoteConfigPath)
}

// deployScript 生成一键部署脚本(端口从清单嵌入,防火墙放行用)。
//
// 调用说明:渲染时把通道端口汇总为 TCP/UDP 两组并嵌入脚本(与 config.json
// 的监听一致,防防火墙放行与实际端口漂移)。
func deployScript(s *conf.Server) (string, error) {
	tcpPorts, udpPorts := firewallPorts(s)
	certBlock, err := certBlockScript(s)
	if err != nil {
		return "", err
	}
	// 校验容器的证书挂载段:config 的 certificate_path 指向容器内
	// /etc/sing/(compose 挂载语义),校验容器不挂证书则 check 读不到
	// 而误报证书缺失;有 H2 才挂(自签/受信两态文件名一致,且第 3 步
	// 证书就位先于校验,挂载不会因缺文件失败)
	checkMounts := ""
	if s.Hysteria2 != nil {
		checkMounts = ` \
  -v "$PWD/cert.pem:/etc/sing/cert.pem:ro" \
  -v "$PWD/key.pem:/etc/sing/key.pem:ro"`
	}
	tcpList := strings.Join(tcpPorts, " ")
	udpList := strings.Join(udpPorts, " ")
	script := fmt.Sprintf(`#!/bin/sh
set -e
cd "$(dirname "$0")"

# 部署用户可能是 root 或普通 sudoer:docker/防火墙命令统一走 sudo -n
# (root 直过、NOPASSWD 免密直过、需密码时快速失败,非交互不卡等待);
# 极简系统可能没有 sudo 二进制(root 直用),command -v 兜底。
SUDO=""
if command -v sudo >/dev/null 2>&1; then
  SUDO="sudo -n"
fi

# 0. 系统防火墙放行端口(云安全组需在控制台放行;vless 走 TCP,
#    hy2 纯 QUIC 仅 UDP)
if command -v ufw >/dev/null 2>&1; then
  for p in %s; do $SUDO ufw allow $p/tcp >/dev/null 2>&1 || true; done
  for p in %s; do $SUDO ufw allow $p/udp >/dev/null 2>&1 || true; done
elif command -v firewall-cmd >/dev/null 2>&1; then
  for p in %s; do $SUDO firewall-cmd --permanent --add-port=$p/tcp >/dev/null 2>&1 || true; done
  for p in %s; do $SUDO firewall-cmd --permanent --add-port=$p/udp >/dev/null 2>&1 || true; done
  $SUDO firewall-cmd --reload >/dev/null 2>&1 || true
fi

# 1. Docker 检查/安装(首次部署自动安装)
if ! command -v docker >/dev/null 2>&1; then
  echo "==> 安装 Docker(首次)..."
  if ! curl -fsSL https://get.docker.com -o /tmp/get-docker.sh; then
    echo "==> 下载 get.docker.com 安装脚本失败(网络不可达或被墙?)" >&2
    echo "==> 请手动安装 Docker 后重跑本脚本" >&2
    exit 1
  fi
  if ! $SUDO sh /tmp/get-docker.sh; then
    echo "==> 官方安装脚本失败(EOL 发行版缺 docker-model-plugin?),回退安装核心包..." >&2
    $SUDO sh -c 'apt-get -qq update && DEBIAN_FRONTEND=noninteractive apt-get -y -qq install docker-ce docker-ce-cli containerd.io docker-compose-plugin docker-buildx-plugin'
  fi
fi

# 1b. Compose v2 子命令检查(老系统存量 docker 可能只有 docker-compose
#     v1 而无 compose 插件,docker compose 子命令不可用;幂等,已有
#     v2 则跳过;非 apt 系发行版安装失败时按提示手动装)
if ! $SUDO docker compose version >/dev/null 2>&1; then
  echo "==> 安装 docker-compose-plugin(Compose v2)..."
  if ! $SUDO sh -c 'apt-get -qq update && DEBIAN_FRONTEND=noninteractive apt-get -y -qq install docker-compose-plugin'; then
    echo "==> docker-compose-plugin 安装失败,请手动安装后重跑本脚本" >&2
    exit 1
  fi
fi

# 2. Docker 开机自启兜底(官方安装分支已 enable,此处覆盖存量机器;
#    非 systemd 环境自动跳过;失败不中断部署,输出警告)
if command -v systemctl >/dev/null 2>&1 && ! systemctl is-enabled docker >/dev/null 2>&1; then
  echo "==> 启用 Docker 开机自启..."
  $SUDO systemctl enable docker >/dev/null 2>&1 || echo "==> 警告:systemctl enable docker 失败,重启后 Docker 不自启,请以 root 手动执行" >&2
fi

# 3. 证书就位(H2 通道;受信证书模式由用户放置 cert.pem/key.pem)
%s
# 4. 镜像就位(ghcr.io 拉取失败回退 docker.io 备源并 retag,compose
#    引用名不变;镜像已存在时跳过,重复部署不重新拉取)
IMAGE=%s
if ! $SUDO docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "==> 拉取镜像 $IMAGE ..."
  if ! $SUDO docker pull "$IMAGE"; then
    echo "==> ghcr.io 拉取失败,尝试备源 %s ..."
    $SUDO docker pull %s
    $SUDO docker tag %s "$IMAGE"
  fi
fi

# 5. 服务端配置语法校验(up 前快速失败,避免启动坏容器后才在日志暴露;
#    H2 通道挂载证书,校验容器与运行容器(compose)挂载语义须一致)
echo "==> 校验服务端配置语法 ..."
$SUDO docker run --rm \
  -v "$PWD/config.json:%s:ro"%s \
  "$IMAGE" check -c %s

# 6. 启动(--force-recreate 强制重建:config.json 内容变化不触发 compose
#    重建,不强制则重新部署后 sing-box 仍跑旧配置)
echo "==> 启动 sing-box ..."
$SUDO docker compose up -d --force-recreate

# 7. 验证(容器状态 + 最近日志,应看到 inbound listening)
sleep 2
$SUDO docker compose ps
$SUDO docker compose logs --tail=10
`,
		tcpList, udpList, tcpList, udpList,
		certBlock,
		ImageRef, ImageMirror, ImageMirror, ImageMirror,
		remoteConfigPath, checkMounts, remoteConfigPath)
	return script, nil
}

// firewallPorts 汇总通道端口:TCP 组(vless)与 UDP 组(hy2)。
func firewallPorts(s *conf.Server) (tcp, udp []string) {
	if s.VLESS != nil {
		tcp = append(tcp, fmt.Sprint(s.VLESS.ListenPort()))
	}
	if s.Hysteria2 != nil {
		udp = append(udp, fmt.Sprint(s.Hysteria2.ListenPort()))
	}
	return tcp, udp
}

// certBlockScript 生成 deploy.sh 的证书就位段(见文件头设计决策)。
func certBlockScript(s *conf.Server) (string, error) {
	h := s.Hysteria2
	if h == nil {
		return "", nil // 无 H2 通道,证书段为空
	}
	if h.ServerName == "" {
		// 自签模式:证书对缺失时用 cert.sh 自动生成
		// (证书存在性成对检查:单边缺失如 key.pem 被误删时重新生成,
		// 防 compose 挂载缺文件导致容器启动失败)
		return `if { [ ! -f cert.pem ] || [ ! -f key.pem ]; }; then
  echo "==> 生成自签证书 ..."
  sh cert.sh
fi
`, nil
	}
	// 受信证书模式:不自动生成,缺证书时报错引导(用户把证书命名为
	// cert.pem/key.pem 放本目录后重跑)
	return `if { [ ! -f cert.pem ] || [ ! -f key.pem ]; }; then
  echo "==> 缺少证书文件 cert.pem/key.pem" >&2
  echo "==> 本服务器配置了 hysteria2.server_name(受信证书模式):" >&2
  echo "==> 请把证书与私钥命名为 cert.pem/key.pem 放入本目录后重跑 deploy.sh" >&2
  exit 1
fi
`, nil
}

// certScript 生成 H2 自签证书脚本(openssl EC 证书,10 年有效期)。
//
// 调用说明:证书生成到产物目录(cert.pem/key.pem,compose 挂载进容器);
// CN 优先 server_name,否则用 address 兜底(纯显示用途,客户端自签
// 场景跳过证书校验,CN 不影响握手)。
func certScript(s *conf.Server) string {
	cn := s.Address
	if s.Hysteria2 != nil && s.Hysteria2.ServerName != "" {
		cn = s.Hysteria2.ServerName
	}
	return fmt.Sprintf(`#!/bin/sh
set -e
# 生成 Hysteria2 自签证书到当前目录(cert.pem/key.pem,docker compose
# 挂载进容器;有效期 10 年,到期重跑本脚本即可,客户端 insecure 跳过
# 证书校验,换证书零影响)
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout key.pem -out cert.pem \
  -days 3650 -nodes -subj /CN=%s
# 私钥收紧 0600(openssl 按 umask 默认 0644,防同机其他用户读取;
# compose 以 root 挂载 :ro 读取,无碍)
chmod 600 key.pem
`, shellQuote(cn))
}

// shellQuote 对 CN 做 POSIX shell 单引号转义(防注入;CN 已由清单校验
// 限定字符集,此处是纵深防御)。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
