package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"tosh/internal/config"
	"tosh/internal/contextdata"
)

func validDraft() string {
	return `{"status":"ready","script":"printf 'safe\\n'","shell":"zsh","suggested_action":"execute","summary":"打印文本","notes":[],"question":""}`
}
func chunk(w http.ResponseWriter, delta any, finish any) {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
}
func final(w http.ResponseWriter, raw, reason string, done bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	mid := len(raw) / 2
	chunk(w, map[string]any{"content": raw[:mid]}, nil)
	chunk(w, map[string]any{"content": raw[mid:]}, nil)
	chunk(w, map[string]any{}, reason)
	if done {
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}
func generate(t *testing.T, handler http.HandlerFunc) (*Draft, error) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	c := Client{Config: config.Config{APIKey: "test-key", BaseURL: srv.URL + "/v1", Model: "test"}, SystemPrompt: "Return JSON"}
	var draft *Draft
	var err error
	c.Generate(context.Background(), Request{Prompt: "test", Context: contextdata.Snapshot{Shell: "zsh", CWD: t.TempDir()}}, func(e Event) {
		if e.Draft != nil {
			draft = e.Draft
		}
		if e.Err != nil {
			err = e.Err
		}
	})
	return draft, err
}
func TestStreamingAndHistoryPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("bad endpoint/auth")
		}
		var p struct {
			Messages []message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		if !strings.Contains(p.Messages[1].Content, "recent-command") || strings.Contains(p.Messages[1].Content, "test-key") {
			t.Error("missing history or exposed key")
		}
		final(w, validDraft(), "stop", true)
	}))
	defer srv.Close()
	c := Client{Config: config.Config{APIKey: "test-key", BaseURL: srv.URL + "/v1", Model: "test"}}
	var got *Draft
	c.Generate(context.Background(), Request{Prompt: "test-key", Context: contextdata.Snapshot{Shell: "zsh", History: []string{"recent-command"}}}, func(e Event) {
		if e.Err != nil {
			t.Error(e.Err)
		}
		if e.Draft != nil {
			got = e.Draft
		}
	})
	if got == nil {
		t.Fatal("no draft")
	}
}
func TestIncompleteAndInvalidResultsNeverReady(t *testing.T) {
	for _, c := range []struct {
		name, raw, reason string
		done              bool
	}{{"truncated", validDraft(), "stop", false}, {"length", validDraft(), "length", true}, {"invalid", "rm -rf /", "stop", true}, {"wrong-shell", strings.Replace(validDraft(), "zsh", "fish", 1), "stop", true}, {"control", strings.Replace(validDraft(), "safe", `\u001b`, 1), "stop", true}} {
		t.Run(c.name, func(t *testing.T) {
			d, e := generate(t, func(w http.ResponseWriter, r *http.Request) { final(w, c.raw, c.reason, c.done) })
			if d != nil || e == nil {
				t.Fatal("unsafe result accepted")
			}
		})
	}
}
func TestToolsRoundTrip(t *testing.T) {
	n := 0
	d, e := generate(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		var p struct {
			Messages []message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]string{"name": "list_directory", "arguments": `{"path":"."}`}}}}, nil)
			chunk(w, map[string]any{}, "tool_calls")
			fmt.Fprint(w, "data: [DONE]\n\n")
		} else {
			last := p.Messages[len(p.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "call-1" {
				t.Error("orphan tool output")
			}
			final(w, validDraft(), "stop", true)
		}
	})
	if e != nil || d == nil || n != 2 {
		t.Fatalf("n=%d error=%v", n, e)
	}
}
func TestUnsupportedToolsFallback(t *testing.T) {
	n := 0
	d, e := generate(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"tools unsupported"}`)
			return
		}
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		if _, ok := p["tools"]; ok {
			t.Error("fallback still has tools")
		}
		final(w, validDraft(), "stop", true)
	})
	if e != nil || d == nil || n != 2 {
		t.Fatal(e)
	}
}
func TestCancellationClosesRequest(t *testing.T) {
	canceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()
	c := Client{Config: config.Config{APIKey: "key", BaseURL: srv.URL, Model: "test"}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c.Generate(ctx, Request{}, func(e Event) {
		if e.Draft != nil {
			t.Error("draft after cancel")
		}
	})
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request not canceled")
	}
}
func TestToolFilesystemBoundary(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	os.WriteFile(outside, []byte("outside-secret"), 0600)
	os.Symlink(outside, filepath.Join(root, "link"))
	os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello"), 0600)
	for _, p := range []string{outside, "link", "../private.txt", ".env", "config.toml"} {
		args, _ := json.Marshal(map[string]string{"path": p})
		if strings.Contains(RunTool(context.Background(), root, "read_file", string(args)), "outside-secret") {
			t.Fatal("filesystem escape")
		}
	}
	if RunTool(context.Background(), root, "read_file", `{"path":"ok.txt"}`) != "hello" {
		t.Fatal("valid read denied")
	}
	if !strings.Contains(RunTool(context.Background(), root, "list_directory", `{"path":"."}`), "ok.txt") {
		t.Fatal("project root listing denied")
	}
	if RunTool(context.Background(), root, "shell", `{"command":"touch x"}`) != "不支持此调查工具" {
		t.Fatal("arbitrary shell tool")
	}
}

func TestModelLanguageDoesNotTranslateUserData(t *testing.T) {
	for _, language := range []string{"zh", "en"} {
		t.Run(language, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p struct {
					Messages []message `json:"messages"`
				}
				json.NewDecoder(r.Body).Decode(&p)
				target := "English"
				if language == "zh" {
					target = "Simplified Chinese"
				}
				if !strings.Contains(p.Messages[0].Content, "Response language: "+target) {
					t.Error("response language missing")
				}
				var data Request
				json.Unmarshal([]byte(p.Messages[1].Content), &data)
				if data.Prompt != "Print 你好 to 文件.txt" {
					t.Error("user literal translated")
				}
				final(w, validDraft(), "stop", true)
			}))
			defer srv.Close()
			c := Client{Config: config.Config{APIKey: "test-key", BaseURL: srv.URL, Model: "test", Language: language}}
			c.Generate(context.Background(), Request{Prompt: "Print 你好 to 文件.txt", Context: contextdata.Snapshot{Shell: "zsh"}}, func(e Event) {
				if e.Err != nil {
					t.Error(e.Err)
				}
			})
		})
	}
}
