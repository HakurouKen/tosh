//go:build darwin || linux

package e2e

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

var (
	liveEnabled    = flag.Bool("live", false, "连接真实模型服务（需要 TOSH_API_KEY、TOSH_BASE_URL、TOSH_MODEL）")
	startupEnabled = flag.Bool("startup", false, "测量 25 次暖启动，检查 P95 <= 200ms")
	buildOnce      sync.Once
	buildDir       string
	buildErr       error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		if err := os.RemoveAll(buildDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// 每个测试进程构建一次当前源码，不读取或覆盖开发者的 bin/tosh。
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "tosh-e2e-build-")
		if buildErr != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(buildDir, "tosh"), "./cmd/tosh")
		cmd.Dir = filepath.Join("..", "..")
		cmd.WaitDelay = time.Second
		if output, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("构建测试二进制: %w\n%s", err, output)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return filepath.Join(buildDir, "tosh")
}

// 使用白名单环境，避免读取真实 history、配置、Shell 初始化文件及 TOSH_*。
func isolatedEnv(directory string, overrides map[string]string) []string {
	values := map[string]string{
		"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "HOME": directory,
		"XDG_CONFIG_HOME": directory, "XDG_DATA_HOME": directory, "ZDOTDIR": directory,
		"TERM": "xterm-256color", "LANG": "C", "LC_ALL": "C", "SHELL": "/bin/sh",
		"HISTFILE": filepath.Join(directory, "history"),
	}
	for key, value := range overrides {
		values[key] = value
	}
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func fixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "config.toml"), "")
	writeFixture(t, filepath.Join(directory, "history"), "echo prior-history\n")
	return directory
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

type session struct {
	t             *testing.T
	master, slave *os.File
	masterFD      int
	before        unix.Termios
	cmd           *exec.Cmd
	done          chan error
	exited        bool
	waitErr       error
	data          []byte
	started       time.Time
	firstFrame    time.Duration
	loading       string
	closed        bool
	colorReplies  int
}

func startSession(t *testing.T, directory, prompt string, env map[string]string) *session {
	t.Helper()
	bin := binary(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, master: master, slave: slave, done: make(chan error, 1), loading: "加载中…"}
	t.Cleanup(s.close)
	before, err := terminalState(slave)
	if err != nil {
		t.Fatal(err)
	}
	s.before = *before
	// 缓存 FD；Go 在部分平台上再次调用 File.Fd 会恢复阻塞模式。
	s.masterFD = int(master.Fd())
	if err := unix.SetNonblock(s.masterFD, true); err != nil {
		t.Fatal(err)
	}
	s.resize(36, 110)
	if _, err := slave.WriteString("PREVIOUS_OUTPUT\r\n"); err != nil {
		t.Fatal(err)
	}
	if env["TOSH_LANG"] == "en" {
		s.loading = "loading…"
	}
	s.cmd = exec.Command(bin, "--config", filepath.Join(directory, "config.toml"), "--shell", "/bin/sh", prompt)
	s.cmd.Dir = directory
	s.cmd.Env = isolatedEnv(directory, env)
	s.cmd.Stdin, s.cmd.Stdout, s.cmd.Stderr = slave, slave, slave
	// 独立进程组方便断言失败或超时时回收 Shell 及其子进程。
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.started = time.Now()
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { s.done <- s.cmd.Wait() }()
	return s
}

func (s *session) close() {
	if s.closed {
		return
	}
	s.closed = true
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		if !s.exited {
			select {
			case s.waitErr = <-s.done:
				s.exited = true
			case <-time.After(3 * time.Second):
				s.t.Error("回收测试子进程超时")
			}
		}
	}
	_ = s.master.Close()
	_ = s.slave.Close()
}

func (s *session) pollExit() bool {
	if !s.exited {
		select {
		case s.waitErr = <-s.done:
			s.exited = true
		default:
		}
	}
	return s.exited
}

func (s *session) read() {
	s.t.Helper()
	fds := []unix.PollFd{{Fd: int32(s.masterFD), Events: unix.POLLIN}}
	if _, err := unix.Poll(fds, 20); err != nil && !errors.Is(err, unix.EINTR) {
		s.t.Fatal(err)
	}
	buf := make([]byte, 65536)
	for i := 0; i < 16; i++ {
		n, err := unix.Read(s.masterFD, buf)
		if n > 0 {
			s.data = append(s.data, buf[:n]...)
			if s.firstFrame == 0 && bytes.Contains(s.data, []byte(s.loading)) {
				s.firstFrame = time.Since(s.started)
			}
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EIO) || n == 0 {
			break
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			s.t.Fatal(err)
		}
	}
	// 查询可能跨多次 read；保留完整缓冲后再匹配。
	query := []byte("\x1b]11;?")
	queries := bytes.Count(s.data, query)
	for s.colorReplies < queries && !s.pollExit() {
		s.send("\x1b]11;rgb:0000/0000/0000\x1b\\")
		s.colorReplies++
	}
}

func (s *session) waitUntil(description string, timeout time.Duration, ready func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for !ready() {
		if time.Now().After(deadline) {
			s.t.Fatalf("等待%s超时\n%s", description, s.tail())
		}
		s.read()
		if s.pollExit() && !ready() {
			s.t.Fatalf("等待%s时进程提前退出: %v\n%s", description, s.waitErr, s.tail())
		}
	}
}

func (s *session) waitFor(text string) {
	s.t.Helper()
	s.waitUntil(text, 5*time.Second, func() bool { return bytes.Contains(s.data, []byte(text)) })
}

func (s *session) send(text string) {
	s.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	data := []byte(text)
	for len(data) > 0 {
		n, err := unix.Write(s.masterFD, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			s.t.Fatal(err)
		}
		if time.Now().After(deadline) {
			s.t.Fatal("写入终端超时")
		}
		if len(data) > 0 {
			fds := []unix.PollFd{{Fd: int32(s.masterFD), Events: unix.POLLOUT}}
			if _, err := unix.Poll(fds, 20); err != nil && !errors.Is(err, unix.EINTR) {
				s.t.Fatal(err)
			}
		}
	}
}

func (s *session) replaceDraft(command string) {
	s.t.Helper()
	// Ctrl+A/Ctrl+K 只清一行。先到输入末尾，再用 Ctrl+U 清除全部行，
	// 每次至少删除一个字符或换行；上限与模型协议的 32768 字节限制一致。
	s.send("\x1b[1;5F" + strings.Repeat("\x15", 32769) + command)
	s.waitFor(command)
}

func (s *session) resize(rows, cols uint16) {
	s.t.Helper()
	if err := pty.Setsize(s.slave, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) signal(sig os.Signal) {
	s.t.Helper()
	if err := s.cmd.Process.Signal(sig); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) tail() string {
	data := s.data
	if len(data) > 2500 {
		data = data[len(data)-2500:]
	}
	return string(data)
}

func (s *session) finish(expected int) string {
	s.t.Helper()
	s.waitUntil("进程退出", 5*time.Second, s.pollExit)
	s.read()
	if s.cmd.ProcessState.ExitCode() != expected {
		s.t.Fatalf("退出码: got %d, want %d (%v)\n%s", s.cmd.ProcessState.ExitCode(), expected, s.waitErr, s.tail())
	}
	after, err := terminalState(s.slave)
	if err != nil {
		s.t.Fatal(err)
	}
	// macOS 可能在恢复 canonical 模式后置 PENDIN；不属于遗留的 raw/echo 状态。
	after.Lflag &^= unix.PENDIN
	s.before.Lflag &^= unix.PENDIN
	if !reflect.DeepEqual(*after, s.before) {
		s.t.Errorf("终端设置未恢复: before=%+v after=%+v", s.before, *after)
	}
	output := string(s.data)
	assertContains(s.t, output, "PREVIOUS_OUTPUT")
	for _, forbidden := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[2J", "\x1b[3J", "\x1b[H"} {
		if strings.Contains(output, forbidden) {
			s.t.Errorf("不应清屏或使用备用屏幕: %q", forbidden)
		}
	}
	const restored = "\x1b[?2004l"
	assertContains(s.t, output, restored)
	tail := output[strings.LastIndex(output, restored)+len(restored):]
	s.close()
	return tail
}

func assertContains(t *testing.T, output, text string) {
	t.Helper()
	if !strings.Contains(output, text) {
		t.Fatalf("输出缺少 %q\n%s", text, output)
	}
}
