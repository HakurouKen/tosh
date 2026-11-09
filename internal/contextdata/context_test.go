package contextdata

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHistoryFormats(t *testing.T) {
	cases := []struct {
		shell, text string
		want        []string
	}{
		{"zsh", ": 1700000000:0;echo one\n: 1700000001:0;echo two\\\ncontinued\n", []string{"echo one", "echo two\ncontinued"}},
		{"bash", "#1700000000\nprintf 'hello'\necho two\n", []string{"printf 'hello'", "echo two"}},
		{"fish", "- cmd: echo one\\necho two\n  when: 1700000000\n- cmd: printf 'ok'\n", []string{"echo one\necho two", "printf 'ok'"}},
	}
	for _, c := range cases {
		t.Run(c.shell, func(t *testing.T) {
			got := ParseHistory(c.text, c.shell)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("%q", got)
			}
		})
	}
}
func TestCollectAlwaysReadsAndRedactsHistory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "history")
	os.WriteFile(p, []byte("echo recent\nAPI_KEY=secret-value curl example\n"), 0600)
	t.Setenv("HISTFILE", p)
	t.Setenv("TMUX", "")
	s := Collect(context.Background(), "/bin/zsh", "test", "")
	if len(s.History) != 2 || s.HistorySource != p || strings.Contains(s.JSON(), "secret-value") {
		t.Fatal("history not collected/redacted")
	}
}
func TestControlSequencesRemoved(t *testing.T) {
	s := Clean("hello\x1b]52;c;YQ==\a\x1b[31mworld\x1b[0m\r")
	if s != "helloworld" {
		t.Fatalf("%q", s)
	}
}
func TestMissingHistoryIsExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HISTFILE", filepath.Join(home, "missing"))
	t.Setenv("ZDOTDIR", home)
	t.Setenv("TMUX", "")
	s := Collect(context.Background(), "/bin/zsh", "test", "")
	if s.HistorySource != "不可用" || s.OutputSource != "不可用" || len(s.Limitations) < 2 {
		t.Fatal("missing history not reported")
	}
}

func TestTailRejectsFIFOWithoutBlocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	if _, _, e := Tail(p, 100); e == nil {
		t.Fatal("FIFO accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("blocked")
	}
}
