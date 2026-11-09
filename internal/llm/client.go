package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"tosh/internal/locale"
	"unicode"

	"tosh/internal/config"
	"tosh/internal/contextdata"
)

type Draft struct {
	Status          string   `json:"status"`
	Script          string   `json:"script"`
	Shell           string   `json:"shell"`
	SuggestedAction string   `json:"suggested_action"`
	Summary         string   `json:"summary"`
	Notes           []string `json:"notes"`
	Question        string   `json:"question"`
}

func ParseDraft(raw, shell string) (Draft, error) {
	var d Draft
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil {
		return d, locale.Fail("Model returned invalid JSON; nothing executed")
	}
	if err := validateShape(fields); err != nil {
		return d, err
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil {
		return d, locale.Fail("Invalid model result fields; nothing executed")
	}
	if d.Shell != shell || (d.SuggestedAction != "execute" && d.SuggestedAction != "copy") || len(d.Script) > 32768 || strings.TrimSpace(d.Summary) == "" || len(d.Summary) > 2048 || len(d.Question) > 4096 || d.Notes == nil || len(d.Notes) > 12 {
		return d, locale.Fail("Model result exceeds limits or targets a different shell; nothing executed")
	}
	for _, r := range d.Script {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return d, locale.Fail("Command contains terminal control characters; nothing executed")
		}
	}
	switch d.Status {
	case "ready":
		if strings.TrimSpace(d.Script) == "" || d.Question != "" {
			return d, locale.Fail("Command draft is incomplete; nothing executed")
		}
	case "needs_input":
		if d.Script != "" || strings.TrimSpace(d.Question) == "" {
			return d, locale.Fail("Invalid clarification result; nothing executed")
		}
	default:
		return d, locale.Fail("Unknown result status; nothing executed")
	}
	for _, n := range d.Notes {
		if len(n) > 2048 {
			return d, locale.Fail("Result notes are too long; nothing executed")
		}
	}
	d.Summary = contextdata.Clean(d.Summary)
	d.Question = contextdata.Clean(d.Question)
	for i := range d.Notes {
		d.Notes[i] = contextdata.Clean(d.Notes[i])
	}
	return d, nil
}

type Event struct {
	Text  string
	Draft *Draft
	Err   error
	Done  bool
	First time.Duration
	Total time.Duration
}
type Request struct {
	Prompt         string               `json:"prompt"`
	Context        contextdata.Snapshot `json:"context"`
	Supplement     string               `json:"supplement,omitempty"`
	PreviousScript string               `json:"previous_script,omitempty"`
}
type Client struct {
	Config       config.Config
	HTTP         *http.Client
	SystemPrompt string
}
type toolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type completion struct {
	Content string
	Calls   []toolCall
	Finish  string
}

func (c *Client) Generate(ctx context.Context, r Request, emit func(Event)) {
	start := time.Now()
	first := time.Duration(0)
	send := func(e Event) {
		if ctx.Err() != nil {
			return
		}
		e.First = first
		e.Total = time.Since(start)
		emit(e)
	}
	defer func() {
		if p := recover(); p != nil {
			send(Event{Err: locale.Fail("Internal generation error; nothing executed"), Done: true})
		}
	}()
	// 在 JSON 编码前遮蔽，确保含引号等特殊字符的密钥也不会漏过。
	redact := func(s string) string {
		s = contextdata.Redact(s)
		if c.Config.APIKey != "" {
			s = strings.ReplaceAll(s, c.Config.APIKey, "[REDACTED]")
		}
		return s
	}
	r.Prompt = redact(r.Prompt)
	r.Supplement = redact(r.Supplement)
	r.PreviousScript = redact(r.PreviousScript)
	r.Context.History = append([]string(nil), r.Context.History...)
	for i := range r.Context.History {
		r.Context.History[i] = redact(r.Context.History[i])
	}
	r.Context.Output = redact(r.Context.Output)
	body, _ := json.Marshal(r)
	data := string(body)
	system := c.SystemPrompt
	for k, v := range map[string]string{"os": runtime.GOOS, "shell": r.Context.Shell, "shell_source": r.Context.ShellSource, "cwd": r.Context.CWD, "available_tools": "list_directory, read_file, port_process", "investigation_budget": "最多 3 轮、6 次工具调用，每次结果最多 16 KiB"} {
		system = strings.ReplaceAll(system, "{{"+k+"}}", v)
	}
	lang, _ := locale.Resolve(c.Config.Language)
	system += "\nResponse language: " + locale.ModelLanguage(lang) + ". Use this language for summary, notes, and question. Accept Chinese and English user prompts. Preserve requested command literals, filenames, paths, and output text exactly; never translate them merely to match the UI language. Keep protocol field names and enum values unchanged."
	messages := []message{{Role: "system", Content: system}, {Role: "user", Content: data}}
	useTools := true
	callsUsed := 0
	onFirst := func() {
		if first == 0 {
			first = time.Since(start)
		}
		send(Event{Text: "模型正在返回结果…"})
	}
	for round := 0; round < 4; round++ {
		if ctx.Err() != nil {
			return
		}
		send(Event{Text: "正在请求模型…"})
		result, err := c.complete(ctx, messages, useTools && round < 3, onFirst)
		var unsupported *unsupportedError
		if errors.As(err, &unsupported) && useTools {
			useTools = false
			send(Event{Text: "服务不支持工具调用，降级为直接生成"})
			result, err = c.complete(ctx, messages, false, onFirst)
		}
		if err != nil {
			send(Event{Err: err, Done: true})
			return
		}
		if len(result.Calls) == 0 {
			if result.Finish != "stop" {
				send(Event{Err: locale.Fail("Model output did not finish normally; nothing executed"), Done: true})
				return
			}
			d, e := ParseDraft(result.Content, r.Context.Shell)
			if e != nil {
				send(Event{Err: e, Done: true})
				return
			}
			send(Event{Draft: &d, Done: true})
			return
		}
		if !useTools || round >= 3 || result.Finish != "tool_calls" || callsUsed+len(result.Calls) > 6 {
			send(Event{Err: locale.Fail("Investigation budget exceeded or protocol incomplete; nothing executed"), Done: true})
			return
		}
		messages = append(messages, message{Role: "assistant", Content: result.Content, ToolCalls: result.Calls})
		ids := map[string]bool{}
		for _, call := range result.Calls {
			if call.ID == "" || ids[call.ID] {
				send(Event{Err: locale.Fail("Invalid tool call identifier; nothing executed"), Done: true})
				return
			}
			ids[call.ID] = true
			callsUsed++
			send(Event{Text: "只读调查：" + contextdata.Clean(call.Function.Name)})
			out := RunTool(ctx, r.Context.CWD, call.Function.Name, call.Function.Arguments)
			out = contextdata.Redact(out)
			out = strings.ReplaceAll(out, c.Config.APIKey, "[REDACTED]")
			out = truncate(out, 16000)
			send(Event{Text: "调查结果：" + truncate(out, 1200)})
			messages = append(messages, message{Role: "tool", ToolCallID: call.ID, Content: out})
		}
	}
	send(Event{Err: locale.Fail("Investigation limit reached; nothing executed"), Done: true})
}

type unsupportedError struct{}

func (*unsupportedError) Error() string { return "服务不支持工具调用" }
func truncate(s string, n int) string {
	if len(s) > n {
		return strings.ToValidUTF8(s[:n], "") + "…（已截断）"
	}
	return s
}

func (c *Client) complete(ctx context.Context, messages []message, tools bool, onFirst func()) (completion, error) {
	var result completion
	payload := map[string]any{"model": c.Config.Model, "messages": messages, "stream": true}
	if tools {
		payload["tools"] = toolDefinitions()
		payload["tool_choice"] = "auto"
	}
	b, _ := json.Marshal(payload)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Config.BaseURL+"/chat/completions", bytes.NewReader(b))
	if e != nil {
		return result, locale.Fail("Cannot create the API request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.Config.APIKey)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, locale.Fail("API connection failed or timed out; check BASE_URL and your proxy")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		s := strings.ToLower(string(b))
		if tools && (resp.StatusCode == 400 || resp.StatusCode == 422) && (strings.Contains(s, "tool") && (strings.Contains(s, "unsupported") || strings.Contains(s, "not support") || strings.Contains(s, "unknown"))) {
			return result, &unsupportedError{}
		}
		return result, locale.Fail("API returned HTTP %d; check the model, credentials, and protocol compatibility", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return result, locale.Fail("API did not return SSE; only streaming Chat Completions is supported")
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 256<<10)
	calls := map[int]*toolCall{}
	done := false
	first := false
	var eventData []string
	total := 0
	consume := func(data string) error {
		if data == "[DONE]" {
			done = true
			return nil
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string     `json:"content"`
					Calls   []toolCall `json:"tool_calls"`
				} `json:"delta"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Error) > 0 {
			return locale.Fail("Invalid API event; nothing executed")
		}
		for _, ch := range chunk.Choices {
			if ch.Index != 0 {
				continue
			}
			if result.Finish != "" && (ch.Delta.Content != "" || len(ch.Delta.Calls) > 0) {
				return locale.Fail("Content received after the finish event; nothing executed")
			}
			if !first && (ch.Delta.Content != "" || len(ch.Delta.Calls) > 0) {
				first = true
				onFirst()
			}
			total += len(ch.Delta.Content)
			if total > 128<<10 {
				return locale.Fail("Model output exceeds the size limit")
			}
			result.Content += ch.Delta.Content
			for _, delta := range ch.Delta.Calls {
				if delta.Index < 0 || delta.Index > 5 {
					return locale.Fail("Too many tool calls")
				}
				call := calls[delta.Index]
				if call == nil {
					call = &toolCall{Type: "function"}
					calls[delta.Index] = call
				}
				call.ID += delta.ID
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
				if len(call.Function.Arguments) > 16384 {
					return locale.Fail("Tool arguments exceed the size limit")
				}
			}
			if ch.Finish != nil {
				result.Finish = *ch.Finish
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if len(eventData) > 0 {
				if e := consume(strings.Join(eventData, "\n")); e != nil {
					return result, e
				}
				eventData = nil
				if done {
					break
				}
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if e := scanner.Err(); e != nil {
		return result, locale.Fail("API stream interrupted; nothing executed")
	}
	if !done || result.Finish == "" {
		return result, locale.Fail("API stream is incomplete; nothing executed")
	}
	var indexes []int
	for i := range calls {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		v := *calls[i]
		v.Index = 0
		result.Calls = append(result.Calls, v)
	}
	return result, nil
}

func ShellName(path string) string { return filepath.Base(path) }
