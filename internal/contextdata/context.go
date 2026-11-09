package contextdata

import (
	"context"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const MaxBytes = 16384

type Snapshot struct {
	CWD           string   `json:"cwd"`
	Shell         string   `json:"shell"`
	ShellSource   string   `json:"shell_source"`
	HistorySource string   `json:"history_source"`
	History       []string `json:"history"`
	Output        string   `json:"output,omitempty"`
	OutputSource  string   `json:"output_source"`
	Limitations   []string `json:"limitations"`
}

func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

var secret = regexp.MustCompile(`(?i)((?:[a-z_]*(?:api[_-]?key|token|password|secret))\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s;]+)`)

func Redact(s string) string {
	s = secret.ReplaceAllString(Clean(s), "${1}[REDACTED]")
	for _, k := range []string{"TOSH_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

func DetectShell(explicit string) (string, string) {
	if explicit != "" {
		return explicit, "显式配置"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	b, e := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(os.Getppid()), "-o", "comm=").Output()
	if e == nil {
		v := strings.TrimSpace(string(b))
		v = strings.TrimPrefix(v, "-")
		n := filepath.Base(v)
		if n == "zsh" || n == "bash" || n == "fish" {
			if filepath.IsAbs(v) {
				return v, "父进程探测"
			}
			if p, e := exec.LookPath(n); e == nil {
				return p, "父进程探测"
			}
		}
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s, "登录 Shell 回退（不保证为当前 Shell）"
	}
	return "/bin/sh", "默认 sh 回退"
}

func Tail(path string, limit int) ([]byte, bool, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, false, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, false, e
	}
	if !st.Mode().IsRegular() {
		return nil, false, os.ErrInvalid
	}
	truncated := st.Size() > int64(limit)
	if truncated {
		_, e = f.Seek(-int64(limit), io.SeekEnd)
		if e != nil {
			return nil, false, e
		}
	}
	b, e := io.ReadAll(io.LimitReader(f, int64(limit)))
	if truncated {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b, truncated, e
}

var zshPrefix = regexp.MustCompile(`^: \d+:\d+;`)

func ParseHistory(text, shell string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	if shell == "fish" {
		for _, l := range lines {
			if strings.HasPrefix(l, "- cmd: ") {
				v := strings.TrimPrefix(l, "- cmd: ")
				v = decodeFish(v)
				out = append(out, Redact(v))
			}
		}
	} else {
		var current string
		for _, l := range lines {
			if shell == "zsh" && strings.HasSuffix(l, `\`) {
				current += zshPrefix.ReplaceAllString(strings.TrimSuffix(l, `\`), "") + "\n"
				continue
			}
			l = current + zshPrefix.ReplaceAllString(l, "")
			current = ""
			if strings.TrimSpace(l) == "" {
				continue
			}
			if strings.HasPrefix(l, "#") {
				if _, e := strconv.ParseInt(strings.TrimPrefix(l, "#"), 10, 64); e == nil {
					continue
				}
			}
			out = append(out, Redact(l))
		}
	}
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}

func decodeFish(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func Collect(ctx context.Context, shell, source, logPath string) Snapshot {
	cwd, _ := os.Getwd()
	s := Snapshot{CWD: cwd, Shell: filepath.Base(shell), ShellSource: source, OutputSource: "不可用", Limitations: []string{"历史文件可能尚未刷盘，未必包含真正的上一条命令"}}
	home, _ := os.UserHomeDir()
	var paths []string
	if p := os.Getenv("HISTFILE"); p != "" {
		paths = append(paths, p)
	}
	switch s.Shell {
	case "zsh":
		root := os.Getenv("ZDOTDIR")
		if root == "" {
			root = home
		}
		paths = append(paths, filepath.Join(root, ".zsh_history"))
	case "fish":
		root := os.Getenv("XDG_DATA_HOME")
		if root == "" {
			root = filepath.Join(home, ".local", "share")
		}
		paths = append(paths, filepath.Join(root, "fish", "fish_history"))
	default:
		paths = append(paths, filepath.Join(home, ".bash_history"))
	}
	for _, p := range paths {
		b, t, e := Tail(p, MaxBytes)
		if e == nil {
			s.HistorySource = p
			s.History = ParseHistory(string(b), s.Shell)
			if t {
				s.Limitations = append(s.Limitations, "history 已截断至近期内容")
			}
			break
		}
	}
	if s.HistorySource == "" {
		s.HistorySource = "不可用"
		s.Limitations = append(s.Limitations, "未能读取历史文件")
	}
	if logPath != "" {
		b, _, e := Tail(logPath, MaxBytes)
		if e == nil {
			s.Output = Redact(string(b))
			s.OutputSource = logPath
		} else {
			s.Limitations = append(s.Limitations, "指定日志读取失败")
		}
	} else if os.Getenv("TMUX") != "" {
		c, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		pane := os.Getenv("TMUX_PANE")
		if !regexp.MustCompile(`^%\d+$`).MatchString(pane) {
			s.Limitations = append(s.Limitations, "无法确定 TMUX_PANE，未读取其他 pane 的输出")
			return s
		}
		cmd := exec.CommandContext(c, "tmux", "capture-pane", "-p", "-t", pane, "-S", "-100")
		cmd.WaitDelay = 50 * time.Millisecond
		buf := &limitedBuffer{}
		cmd.Stdout = buf
		if cmd.Run() == nil {
			s.Output = Redact(buf.String())
			s.OutputSource = "tmux 当前 pane 最近 100 行（可能包含提示符及多条命令）"
		}
	}
	if s.OutputSource == "不可用" {
		s.Limitations = append(s.Limitations, "未取得此前输出；不会重跑历史命令")
	}
	return s
}
func (s Snapshot) JSON() string { b, _ := json.MarshalIndent(s, "", "  "); return string(b) }

type limitedBuffer struct{ s strings.Builder }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := MaxBytes - b.s.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.s.Write(p)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { return b.s.String() }
