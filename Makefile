# vpn 域验证与部署命令固化
#
# 覆盖本项目全部包:
#   - cmd/vpn          CLI 入口(gen/deploy/doctor 子命令)
#   - internal/conf    清单模型(校验/凭据回填/IO)
#   - internal/server  服务端产物生成(sing-box 配置 + 编排/部署脚本)
#   - internal/client  客户端产物生成(链接/订阅/sing-box 配置)
#   - internal/deploy  远程部署执行(ssh/scp)
#
# 零第三方 Go 依赖:全部标准库,构建秒级,无构建标签负担。
# 使用:cd 项目根目录后执行 make doctor / make gen / make deploy / make deploy-openwrt / make check
OUT ?= dist
INVENTORY ?= servers.json

.PHONY: doctor gen deploy deploy-openwrt check build vet test test-race fmt clean

# doctor 环境自检(Go 1.27+ / ssh / scp / openssl)
doctor:
	go run ./cmd/vpn doctor

# gen 生成全部产物(服务端 + 客户端)。
# 首次执行:清单不存在时自动复制示例并提示编辑(exit 1,不生成产物)。
# 幂等:再次 gen 复用清单已回填凭据,产物不变。
gen:
	@test -f "$(INVENTORY)" || (cp example-servers.json "$(INVENTORY)" && echo "已复制示例清单到 $(INVENTORY),请按 docs/server.md 编辑每台服务器的 address 后重新执行 make gen" && exit 1)
	go run ./cmd/vpn gen -inventory "$(INVENTORY)" -out "$(OUT)"

# deploy 一条命令上传 + 远程部署全部服务器(依赖 gen 产物)。
# 错误即停:任一台失败立即返回(错误含服务器名),修复后重跑安全(deploy.sh 幂等)。
deploy:
	@test -f "$(INVENTORY)" || (echo "清单 $(INVENTORY) 不存在,先执行 make gen" && exit 1)
	go run ./cmd/vpn deploy -inventory "$(INVENTORY)" -out "$(OUT)"

# deploy-openwrt 一键部署 OpenWrt 网关盒子(依赖 gen 产物 sing-box-openwrt.json;
# 目标单台、不入清单,直接指定地址)。
# 用法:make deploy-openwrt HOST=192.168.1.1 [PORT=2222](PORT 缺省 22,user 缺省 root)
HOST ?=
PORT ?= 22
deploy-openwrt:
	@test -f "$(OUT)/sing-box-openwrt.json" || (echo "产物 $(OUT)/sing-box-openwrt.json 不存在,先执行 make gen" && exit 1)
	@test -n "$(HOST)" || (echo "缺少目标地址:make deploy-openwrt HOST=<盒子IP> [PORT=<端口>]" && exit 1)
	go run ./cmd/vpn openwrt -host "$(HOST)" -p "$(PORT)" -out "$(OUT)"

# check 全量验证(build + vet + test + fmt)
check: build vet test fmt

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./... -count=1

# test-race 竞态检测(部署/生成均单线程,无共享状态,常规不跑)
test-race:
	go test -race ./... -count=1

# fmt 格式检查(gofmt -l 非空即失败,不自动改写)
fmt:
	@unfmt="$$(gofmt -l .)"; \
	if [ -n "$$unfmt" ]; then \
		echo "以下文件未格式化(go fmt <文件> 修复后重跑 make fmt):"; \
		echo "$$unfmt"; \
		exit 1; \
	fi; \
	echo "gofmt 干净"

# clean 清理生成产物(清单 servers.json 保留,凭据不丢)
clean:
	rm -rf "$(OUT)"
