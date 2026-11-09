package execution

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
	"tosh/internal/locale"
	"unicode"
)

var stateCommand = regexp.MustCompile(`^(cd|export|unset|source|alias|unalias|function|set|setenv|eval|exec|exit)(\s|$)`)

// 仅提供保守建议；任意 Shell 语义无法通过简单分类完整判断。
func SuggestedAction(script string) string {
	s := strings.TrimSpace(script)
	if stateCommand.MatchString(s) && !strings.ContainsAny(s, "\n;|&") {
		return "copy"
	}
	return "execute"
}
func Validate(script string) error {
	if strings.TrimSpace(script) == "" || len(script) > 32768 {
		return locale.Fail("Command is empty or too long")
	}
	for _, r := range script {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return locale.Fail("Command contains control characters; nothing executed")
		}
	}
	return nil
}
func Copy(script string, out io.Writer, lang locale.Language) error {
	if err := Validate(script); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pbcopy")
	cmd.Stdin = strings.NewReader(script)
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(out, locale.Text(lang, "Clipboard unavailable; copy this command manually. It has not been executed:")+"\n"+script)
		return nil
	}
	fmt.Fprintln(out, locale.Text(lang, "Copied, not executed. Paste into your current shell to run."))
	return nil
}
func Execute(shell, script string) error {
	if err := Validate(script); err != nil {
		return err
	}
	p, err := exec.LookPath(shell)
	if err != nil {
		return locale.Fail("Target shell is unavailable")
	}
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "TOSH_API_KEY=") {
			env = append(env, v)
		}
	}
	// 在 TUI 已恢复的终端中原样回显最终文本，再执行同一份命令。
	if _, err := fmt.Fprintln(os.Stdout, script); err != nil {
		return locale.Fail("Cannot print the command; nothing executed")
	}
	signal.Reset()
	// 替换进程，交互命令直接获得原终端及前台进程组。
	return syscall.Exec(p, []string{p, "-c", script}, env)
}
