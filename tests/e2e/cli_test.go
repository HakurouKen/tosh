//go:build darwin || linux

package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCLI(t *testing.T) {
	bin := binary(t)
	for _, tc := range []struct {
		name, config, language, systemLocale, expected string
		args                                           []string
	}{
		{name: "HelpWithoutCredentials", expected: "Ctrl+X", args: []string{"-h"}},
		{name: "FlagEnglish", language: "zh", expected: "Usage:", args: []string{"--lang", "en", "-h"}},
		{name: "FlagChinese", language: "en", expected: "用法：", args: []string{"--lang", "zh", "-h"}},
		{name: "ConfigChinese", config: "LANGUAGE=\"zh\"\n", expected: "用法：", args: []string{"-h"}},
		{name: "EnvOverridesConfig", config: "LANGUAGE=\"zh\"\n", language: "en", expected: "Usage:", args: []string{"-h"}},
		{name: "EnglishDefault", systemLocale: "zh_CN.UTF-8", expected: "Usage:", args: []string{"-h"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := fixture(t)
			path := filepath.Join(directory, "config.toml")
			writeFixture(t, path, tc.config)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, append([]string{"--config", path}, tc.args...)...)
			cmd.Dir = directory
			env := map[string]string{"TOSH_LANG": tc.language}
			if tc.systemLocale != "" {
				env["LC_ALL"] = tc.systemLocale
			}
			cmd.Env = isolatedEnv(directory, env)
			cmd.WaitDelay = time.Second
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("帮助命令失败: %v\n%s", err, output)
			}
			assertContains(t, string(output), tc.expected)
			assertContains(t, string(output), "API_KEY")
		})
	}
}
