package locale

import (
	"strings"
	"testing"
)

func TestResolveLocalePrecedence(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got, _ := Resolve("auto"); got != Chinese {
		t.Fatal("LANG not detected")
	}
	t.Setenv("LC_MESSAGES", "en_US.UTF-8")
	if got, _ := Resolve("auto"); got != English {
		t.Fatal("LC_MESSAGES not preferred")
	}
	t.Setenv("LC_ALL", "zh_TW.UTF-8")
	if got, _ := Resolve(""); got != English {
		t.Fatal("default must be English even under a Chinese system locale")
	}
	if got, _ := Resolve("auto"); got != Chinese {
		t.Fatal("LC_ALL not preferred")
	}
	if got, _ := Resolve("en"); got != English {
		t.Fatal("explicit language not preferred")
	}
	if _, err := Resolve("fr"); err == nil {
		t.Fatal("invalid explicit language accepted")
	}
	t.Setenv("LC_ALL", "C.UTF-8")
	if got, _ := Resolve("auto"); got != English {
		t.Fatal("C locale must use English")
	}
}

func TestFormattedErrorsPreserveArguments(t *testing.T) {
	err := Fail("Cannot read configuration: %s", "/tmp/中文/config.toml")
	if err.Error() != "Cannot read configuration: /tmp/中文/config.toml" {
		t.Fatal("raw error must default to English")
	}
	if got := ErrorText(English, err); got != "Cannot read configuration: /tmp/中文/config.toml" {
		t.Fatal(got)
	}
	if got := ErrorText(Chinese, err); got != "无法读取配置：/tmp/中文/config.toml" {
		t.Fatal(got)
	}
	if !strings.Contains(Help(English, "/tmp/config"), "Usage:") || !strings.Contains(Help(Chinese, "/tmp/config"), "用法：") {
		t.Fatal("missing localized help")
	}
}

func TestUntranslatedKeysDefaultToEnglish(t *testing.T) {
	if Text(English, "A new message") != "A new message" || Text(Chinese, "A new message") != "A new message" {
		t.Fatal("missing translations must preserve the English key")
	}
	if Text(Chinese, "Show version") != "显示版本" {
		t.Fatal("Chinese value not selected")
	}
}
