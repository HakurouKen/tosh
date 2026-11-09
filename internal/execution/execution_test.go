package execution

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExecHelperProcess(t *testing.T) {
	if os.Getenv("TOSH_EXEC_HELPER") != "1" {
		return
	}
	if Execute(os.Getenv("TOSH_TEST_SHELL"), os.Getenv("TOSH_TEST_SCRIPT")) != nil {
		os.Exit(99)
	}
}

func TestInstalledShells(t *testing.T) {
	for _, name := range []string{"zsh", "bash", "fish", "sh"} {
		t.Run(name, func(t *testing.T) {
			path, e := exec.LookPath(name)
			if e != nil {
				t.Skip("Shell 未安装")
			}
			for _, tc := range []struct {
				script, want string
				code         int
			}{
				{"printf '%s\\n' '你好 quoted value'\nprintf '%s' second", "你好 quoted value\nsecond", 0},
				{"exit 7", "", 7},
				{"printenv TOSH_API_KEY", "", 1},
			} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestExecHelperProcess$")
				cmd.Env = append(os.Environ(), "TOSH_EXEC_HELPER=1", "TOSH_TEST_SHELL="+path, "TOSH_TEST_SCRIPT="+tc.script, "TOSH_API_KEY=execution-test-secret")
				out, e := cmd.CombinedOutput()
				code := 0
				if e != nil {
					if v, ok := e.(*exec.ExitError); ok {
						code = v.ExitCode()
					} else {
						t.Fatal(e)
					}
				}
				if string(out) != tc.script+"\n"+tc.want || code != tc.code {
					t.Fatalf("shell=%s code=%d output=%q", name, code, strings.ReplaceAll(string(out), "execution-test-secret", "[REDACTED]"))
				}
			}
		})
	}
}

func TestParentStateClassification(t *testing.T) {
	for _, s := range []string{"cd /tmp", "export X=1", "set -gx X 1", "source foo"} {
		if SuggestedAction(s) != "copy" {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"cd /tmp && pwd", "printf hello", "cd /tmp\npwd"} {
		if SuggestedAction(s) != "execute" {
			t.Fatal(s)
		}
	}
}
func TestRejectControlInEditedScript(t *testing.T) {
	if Validate("printf hi\x1b[31m") == nil {
		t.Fatal("control accepted")
	}
	if Validate("printf '你好'\npwd") != nil {
		t.Fatal("valid multiline rejected")
	}
}
