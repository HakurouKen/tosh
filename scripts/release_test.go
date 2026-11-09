//go:build darwin || linux

package scripts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repository struct {
	t   *testing.T
	dir string
}

func fixture(t *testing.T) repository {
	t.Helper()
	r := repository{t, t.TempDir()}
	for _, file := range []string{"Makefile", "VERSION", ".gitignore", "LICENSE", "README.md", "scripts/release.sh"} {
		data, err := os.ReadFile(filepath.Join("..", file))
		if err != nil {
			t.Fatal(err)
		}
		r.write(file, string(data))
	}
	// 使用最小 Go 程序测试真实编译、打包和发布流程，不依赖模型服务。
	r.write("go.mod", "module releasefixture\n\ngo 1.25.6\n")
	r.write("cmd/tosh/main.go", "package main\nimport \"fmt\"\nvar version = \"dev\"\nfunc main() { fmt.Println(\"tosh \" + version) }\n")
	r.run(true, "git", "init", "-q")
	r.run(true, "git", "config", "user.name", "Release Test")
	r.run(true, "git", "config", "user.email", "release@example.invalid")
	r.commit()
	return r
}

func (r repository) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		r.t.Fatal(err)
	}
}

func (r repository) run(success bool, name string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := cmd.CombinedOutput()
	if (err == nil) != success {
		r.t.Fatalf("%s %q: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (r repository) commit() {
	r.t.Helper()
	r.run(true, "git", "add", ".")
	r.run(true, "git", "commit", "-qm", "fixture")
}

func TestVersionBump(t *testing.T) {
	for _, tc := range []struct{ part, want string }{
		{"patch", "0.1.1"}, {"minor", "0.2.0"}, {"major", "1.0.0"},
	} {
		t.Run(tc.part, func(t *testing.T) {
			r := fixture(t)
			head := r.run(true, "git", "rev-parse", "HEAD")
			r.run(true, "make", "bump", "PART="+tc.part)
			if got := r.run(true, "make", "-s", "version"); got != tc.want {
				t.Fatalf("版本 = %s，期望 %s", got, tc.want)
			}
			if got := r.run(true, "git", "diff", "--name-only"); got != "VERSION" {
				t.Fatalf("bump 修改了额外文件：%s", got)
			}
			if r.run(true, "git", "rev-parse", "HEAD") != head || r.run(true, "git", "tag") != "" {
				t.Fatal("bump 不应提交或打 tag")
			}
		})
	}
	for _, invalid := range []string{"v0.1.0", "01.2.3", "1.2.3-rc.1", "1.2.3\n4.5.6", "../dist"} {
		t.Run(invalid, func(t *testing.T) {
			r := fixture(t)
			r.write("VERSION", invalid+"\n")
			r.run(false, "make", "bump")
		})
	}
	t.Run("InvalidPart", func(t *testing.T) {
		r := fixture(t)
		r.run(false, "make", "bump", "PART=invalid")
		if got := r.run(true, "make", "-s", "version"); got != "0.1.0" {
			t.Fatalf("非法 bump 修改了版本：%s", got)
		}
	})
}

func TestTagAndReleaseGuards(t *testing.T) {
	r := fixture(t)
	r.run(false, "make", "release") // 缺少 tag。
	r.write("untracked", "draft")
	r.run(false, "make", "tag")
	r.commit()
	r.run(true, "make", "tag")
	if got := r.run(true, "git", "cat-file", "-t", "v0.1.0"); got != "tag" {
		t.Fatalf("不是 annotated tag：%s", got)
	}
	r.run(false, "make", "tag") // 拒绝覆盖。
	r.write("untracked", "changed")
	r.run(false, "make", "release") // 已跟踪文件有修改。
	r.commit()
	r.run(false, "make", "release") // tag 未指向 HEAD。
	if got := r.run(true, "git", "tag"); got != "v0.1.0" {
		t.Fatalf("检查失败不应创建额外 tag：%s", got)
	}
}

func TestDistributionAndOfflineRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过四个平台真实构建与离线发布")
	}
	r := fixture(t)
	// URL 重写只作用于临时仓库，所有 push 都落在本地 bare 仓库。
	remote := filepath.Join(t.TempDir(), "origin.git")
	r.run(true, "git", "init", "--bare", "-q", remote)
	r.run(true, "git", "remote", "add", "origin", "https://github.com/HakurouKen/tosh.git")
	r.run(true, "git", "config", "url."+remote+".insteadOf", "https://github.com/HakurouKen/tosh.git")
	// get-url 会应用 insteadOf；pushurl 单独使用原始地址供发布检查，push 仍使用 insteadOf。
	// 使用 PATH 中的 git 包装器，仅让 get-url 返回规范地址。
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	toolsDir := t.TempDir()
	log := filepath.Join(t.TempDir(), "gh.log")
	for name, script := range map[string]string{
		"gh":  "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_TEST_LOG\"\n",
		"git": "#!/bin/sh\nif [ \"$*\" = 'remote get-url --push origin' ]; then\n echo https://github.com/HakurouKen/tosh.git\nelse\n exec \"$REAL_TEST_GIT\" \"$@\"\nfi\n",
	} {
		if err := os.WriteFile(filepath.Join(toolsDir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TEST_LOG", log)
	t.Setenv("REAL_TEST_GIT", gitPath)
	r.run(true, "make", "tag")
	// 发布前的测试失败时，不能推送 tag 或创建线上 Release。
	goStub := filepath.Join(toolsDir, "go")
	if err := os.WriteFile(goStub, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	r.run(false, "make", "release")
	r.run(false, "git", "--git-dir="+remote, "show-ref", "--verify", "refs/tags/v0.1.0")
	failedLog, err := os.ReadFile(log)
	if err != nil || strings.Contains(string(failedLog), "release create") {
		t.Fatalf("测试失败不应创建 Release：%v\n%s", err, failedLog)
	}
	if err := os.Remove(goStub); err != nil {
		t.Fatal(err)
	}
	r.run(true, "make", "release")
	if got := r.run(true, "git", "--git-dir="+remote, "rev-parse", "v0.1.0^{commit}"); got != r.run(true, "git", "rev-parse", "HEAD") {
		t.Fatal("推送的 tag 不对应当前提交")
	}
	for _, target := range []string{"darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"} {
		name := "tosh_0.1.0_" + target + ".tar.gz"
		data, err := os.ReadFile(filepath.Join(r.dir, "dist/v0.1.0", name))
		if err != nil {
			t.Fatal(err)
		}
		checksums, err := os.ReadFile(filepath.Join(r.dir, "dist/v0.1.0/checksums.txt"))
		if err != nil || !strings.Contains(string(checksums), fmt.Sprintf("%x  ./%s", sha256.Sum256(data), name)) {
			t.Fatalf("校验文件不匹配 %s: %v", name, err)
		}
		checkArchive(t, filepath.Join(r.dir, "dist/v0.1.0", name))
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"auth status", "release create v0.1.0", "--verify-tag", "--generate-notes", "checksums.txt"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("gh 调用缺少 %s：%s", expected, data)
		}
	}
}

func checkArchive(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	found := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		found[strings.TrimPrefix(header.Name, "./")] = true
	}
	for _, required := range []string{"tosh", "README.md", "LICENSE"} {
		if !found[required] {
			t.Fatalf("%s 缺少 %s", path, required)
		}
	}
}
