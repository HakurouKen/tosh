//go:build darwin || linux

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"tosh/internal/llm"

	"golang.org/x/sys/unix"
)

func mockAPI(t *testing.T) (string, <-chan string) {
	t.Helper()
	requests := make(chan string, 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil || len(request.Messages) < 2 {
			http.Error(w, "invalid messages", http.StatusBadRequest)
			return
		}
		var user struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &user); err != nil {
			http.Error(w, "invalid user message", http.StatusBadRequest)
			return
		}
		select {
		case requests <- request.Messages[1].Content:
		default:
			t.Error("模拟 API 请求超出缓冲容量")
		}
		if user.Prompt == "slow" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
		draft := llm.Draft{
			Status: "ready", Script: "printf 'PTY_EXEC_OK\\n'", Shell: "sh",
			SuggestedAction: "execute", Summary: "模拟服务已生成", Notes: []string{},
		}
		switch user.Prompt {
		case "interactive":
			draft.Script = `read -r answer; printf 'ANSWER:%s\n' "$answer"`
		case "copy":
			draft.Script, draft.SuggestedAction = "cd /tmp", "copy"
		case "exitcode":
			draft.Script = "exit 7"
		case "multiline":
			draft.Script = "printf 'UNEDITED_FIRST\\n'\nprintf 'UNEDITED_LAST\\n'"
		}
		result, err := json.Marshal(draft)
		if err != nil {
			t.Error(err)
			return
		}
		if user.Prompt == "bad" {
			result = []byte("invalid output")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{string(result[:len(result)/2]), string(result[len(result)/2:])} {
			event, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"index": 0, "delta": map[string]string{"content": part}, "finish_reason": nil,
			}}})
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server.URL + "/v1", requests
}

func mockSession(t *testing.T, prompt, language string) (*session, <-chan string) {
	t.Helper()
	directory := fixture(t)
	url, requests := mockAPI(t)
	s := startSession(t, directory, prompt, map[string]string{
		"TOSH_API_KEY": "mock-only", "TOSH_BASE_URL": url, "TOSH_MODEL": "mock", "TOSH_LANG": language,
		// 无论宿主机是否安装 pbcopy，都覆盖复制失败的路径。
		"PATH": directory,
	})
	return s, requests
}

func TestPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 跳过 PTY 端到端测试")
	}
	t.Run("EditAndExecute", func(t *testing.T) {
		s, requests := mockSession(t, "normal", "zh")
		s.waitFor("printf 'PTY_EXEC_OK")
		s.send("\x01\x0bprintf 'EDITED_OK\\n'")
		s.waitFor("printf 'EDITED_OK")
		s.send("\r")
		assertContains(t, s.finish(0), "printf 'EDITED_OK\\n'\r\nEDITED_OK\r\n")
		select {
		case request := <-requests:
			assertContains(t, request, "prior-history")
		default:
			t.Fatal("未收到模型请求")
		}
	})
	t.Run("PasteResizeAndCancel", func(t *testing.T) {
		s, _ := mockSession(t, "normal", "zh")
		s.waitFor("printf 'PTY_EXEC_OK")
		s.send("\x1b[200~\nprintf 'PASTE_BAD\\n'\n\x1b[201~")
		// 粘贴以空行结尾，textarea 可能只展示最后一行；用后续输入同步。
		s.send("PASTE_END")
		s.waitFor("PASTE_END")
		if s.pollExit() {
			t.Fatal("粘贴触发了执行")
		}
		s.resize(15, 45)
		s.signal(syscall.SIGWINCH)
		// 缩放后的新编辑仍可渲染，证明事件循环继续处理输入。
		s.send("RESIZE_OK")
		s.waitFor("RESIZE_OK")
		s.send("\x03")
		if tail := s.finish(0); strings.Contains(tail, "PASTE_BAD") || strings.Contains(tail, "RESIZE_OK") {
			t.Fatalf("取消后执行了编辑内容: %s", tail)
		}
	})
	t.Run("ReplaceMultilineDraft", func(t *testing.T) {
		s, _ := mockSession(t, "multiline", "zh")
		s.waitFor("UNEDITED_LAST")
		const command = "printf 'SAFE_REPLACEMENT\\n' # e2e-confirmed"
		s.replaceDraft(command)
		s.send("\r")
		tail := s.finish(0)
		assertContains(t, tail, command+"\r\nSAFE_REPLACEMENT\r\n")
		if strings.Contains(tail, "UNEDITED_") {
			t.Fatalf("替换后仍执行了模型草稿: %s", tail)
		}
	})
	t.Run("InteractiveStdin", func(t *testing.T) {
		s, _ := mockSession(t, "interactive", "zh")
		s.waitFor("read -r answer")
		s.send("\r")
		s.waitFor("\x1b[?2004l")
		s.waitUntil("恢复 canonical 模式", 5*time.Second, func() bool {
			state, err := terminalState(s.slave)
			if err != nil {
				t.Fatal(err)
			}
			return state.Lflag&unix.ICANON != 0
		})
		s.send("terminal-input\r")
		assertContains(t, s.finish(0), "ANSWER:terminal-input")
	})
	for _, language := range []string{"zh", "en"} {
		t.Run("CopyFallback/"+language, func(t *testing.T) {
			s, _ := mockSession(t, "copy", language)
			s.waitFor("cd /tmp")
			s.send("\r")
			expected := "尚未执行"
			if language == "en" {
				expected = "Clipboard unavailable"
			}
			assertContains(t, s.finish(0), expected)
		})
		t.Run("InvalidResult/"+language, func(t *testing.T) {
			s, requests := mockSession(t, "bad", language)
			expected := "模型未返回有效 JSON"
			if language == "en" {
				expected = "Model returned invalid JSON"
			}
			s.waitFor(expected)
			<-requests // 首次响应已呈现，请求必然已入队。
			s.send("\t\r")
			// 相同错误文案可能被 renderer 去重，以实际重试请求作为同步点。
			retried := false
			s.waitUntil("无效结果触发重试", 5*time.Second, func() bool {
				select {
				case <-requests:
					retried = true
				default:
				}
				return retried
			})
			if s.pollExit() {
				t.Fatal("无效结果被确认执行")
			}
			s.send("\x03")
			s.finish(0)
		})
	}
	t.Run("SignalRestoresTerminal", func(t *testing.T) {
		s, _ := mockSession(t, "slow", "zh")
		s.waitFor(s.loading)
		s.signal(syscall.SIGTERM)
		s.finish(0)
	})
	t.Run("ExitCode", func(t *testing.T) {
		s, _ := mockSession(t, "exitcode", "zh")
		s.waitFor("exit 7")
		offset := len(s.data)
		s.send("\x18")
		s.waitUntil("切换执行模式", 5*time.Second, func() bool { return strings.Contains(string(s.data[offset:]), "›") })
		s.send("\r")
		s.finish(7)
	})
}
