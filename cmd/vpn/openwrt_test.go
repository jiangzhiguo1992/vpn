// Package main cmd/vpn openwrt 子命令参数校验测试:缺 -host(含纯空白)、
// -p 越界(0/70000)均在调用 deploy.RunOpenWrt 前返回错误,不触发任何
// 真实部署(错误信息不应出现部署层文案,如"先 make gen")。
package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRunOpenWrt_参数校验失败 验证非法参数在参数校验阶段即返回,不触碰
// deploy 层(校验在调用 deploy.RunOpenWrt 之前完成)。
func TestRunOpenWrt_参数校验失败(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "dist")
	cases := []struct {
		name       string
		args       []string
		wantPhrase string
	}{
		{"缺 -host", []string{"-out", outDir}, "缺少必填参数 -host"},
		{"-host 纯空白", []string{"-host", "   ", "-out", outDir}, "缺少必填参数 -host"},
		{"-p 0 越界", []string{"-host", "root@192.168.1.1", "-p", "0", "-out", outDir}, "1..65535"},
		{"-p 70000 越界", []string{"-host", "root@192.168.1.1", "-p", "70000", "-out", outDir}, "1..65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runOpenWrt(tc.args)
			if err == nil {
				t.Fatal("期望参数校验失败,实际 nil")
			}
			if !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Fatalf("错误应含 %q: %v", tc.wantPhrase, err)
			}
			// 参数校验须在 deploy 之前返回:出现部署层专属文案(产物检查/
			// scp/ssh 阶段)说明已触达 RunOpenWrt,本测试即应失败
			for _, forbidden := range []string{"产物不存在", "上传配置失败", "远程部署失败"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("参数校验应早于 deploy 调用(出现部署层文案 %q): %v", forbidden, err)
				}
			}
		})
	}
}
