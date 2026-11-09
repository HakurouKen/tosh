//go:build darwin || linux

package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	if !*liveEnabled {
		t.Skip("使用 -live 显式启用真实服务冒烟测试")
	}
	if testing.Short() {
		t.Fatal("-live 不能与 -short 同时使用")
	}
	env := map[string]string{"TOSH_LANG": "zh"}
	for _, key := range []string{"TOSH_API_KEY", "TOSH_BASE_URL", "TOSH_MODEL"} {
		value := os.Getenv(key)
		if strings.TrimSpace(value) == "" {
			t.Fatalf("启用 live 测试需要设置 %s", key)
		}
		env[key] = value
	}
	// 允许真实服务使用宿主机的代理及自定义 CA；仍不继承 history 或其他 TOSH_*。
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	directory := fixture(t)
	writeFixture(t, filepath.Join(directory, "README.md"), "# Live test fixture\n")
	prompt := "只生成一行 printf 命令，打印 tosh-live-ok。不调查。"
	if os.Getenv("TOSH_LIVE_INVESTIGATE") == "1" {
		prompt = "先调用 list_directory 工具确认当前目录包含 README.md，然后只生成一行 printf 命令打印 tosh-live-ok。"
	}
	s := startSession(t, directory, prompt, env)
	s.waitUntil("真实服务生成命令", 120*time.Second, func() bool { return bytes.Contains(s.data, []byte("tosh-live-ok")) })
	// 只执行测试写入的固定命令，不直接执行模型草稿。
	const command = "printf 'tosh-live-ok\\n' # e2e-confirmed"
	s.replaceDraft(command)
	s.send("\r")
	assertContains(t, s.finish(0), command+"\r\ntosh-live-ok\r\n")
}

func TestStartup(t *testing.T) {
	if !*startupEnabled {
		t.Skip("使用 -startup 显式启用启动性能测试")
	}
	if testing.Short() {
		t.Fatal("-startup 不能与 -short 同时使用")
	}
	// 先完成构建和一次预热，采样不包含编译时间。
	binary(t)
	url, _ := mockAPI(t)
	directory := fixture(t)
	env := map[string]string{"TOSH_API_KEY": "mock-only", "TOSH_BASE_URL": url, "TOSH_MODEL": "mock", "TOSH_LANG": "zh"}
	var samples []time.Duration
	for i := 0; i < 26; i++ {
		s := startSession(t, directory, "slow", env)
		s.waitFor(s.loading)
		if i > 0 {
			samples = append(samples, s.firstFrame)
		}
		s.send("\x03")
		s.finish(0)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[23] // nearest-rank: ceil(0.95 * 25) - 1
	t.Logf("暖启动 n=%d, median=%s, P95=%s", len(samples), samples[len(samples)/2], p95)
	if p95 > 200*time.Millisecond {
		t.Fatalf("启动 P95 %s 超过 200ms", p95)
	}
}
