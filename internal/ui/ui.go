package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"tosh/internal/contextdata"
	"tosh/internal/execution"
	"tosh/internal/llm"
	"tosh/internal/locale"
)

type Generator interface {
	Generate(context.Context, llm.Request, func(llm.Event))
}
type streamMsg struct {
	Event      llm.Event
	Generation int
}
type Options struct {
	Language  locale.Language
	Prompt    string
	Generator Generator
	Collect   func(context.Context) contextdata.Snapshot
}
type Result struct {
	Script string
	Action string
}
type Model struct {
	opts          Options
	editor        textarea.Model
	spinner       spinner.Model
	phase         string
	prompt        string
	supplement    string
	notice        string
	action        string
	width, height int
	generation    int
	ch            chan streamMsg
	cancel        context.CancelFunc
	done          chan struct{}
	Result        Result
}

func New(opts Options) *Model {
	ed := textarea.New()
	ed.Placeholder = ""
	ed.CharLimit = 32768
	ed.ShowLineNumbers = false
	ed.Prompt = "› "
	ed.SetWidth(72)
	ed.SetHeight(1)
	ed.Focus()
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	m := &Model{opts: opts, editor: ed, spinner: spin, phase: "input", width: 80, height: 24, prompt: opts.Prompt}
	if opts.Prompt != "" {
		m.phase = "pending"
	}
	return m
}
func (m *Model) Init() tea.Cmd {
	if m.phase == "pending" {
		return m.begin()
	}
	return textarea.Blink
}
func wait(ch <-chan streamMsg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return v
	}
}
func (m *Model) resizeEditor() {
	m.editor.SetWidth(max(4, min(m.width-4, 120)))
	m.editor.SetHeight(max(1, min(8, m.editor.LineCount(), m.height-1)))
}

// Dismiss 只清理本程序渲染的区域，保留之前的终端内容。
func (m *Model) Dismiss() { m.phase = "closed" }

func (m *Model) syncAction() {
	m.editor.Prompt = "› "
	if m.action == "copy" {
		m.editor.Prompt = "⎘ "
	}
}
func (m *Model) begin() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	m.generation++
	id := m.generation
	m.phase = "generating"
	m.notice = ""
	m.editor.Blur()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.ch = make(chan streamMsg, 16)
	m.done = make(chan struct{})
	ch, done := m.ch, m.done
	r := llm.Request{Prompt: m.prompt, Supplement: m.supplement}
	collect, generator := m.opts.Collect, m.opts.Generator
	go func() {
		defer close(done)
		defer close(ch)
		r.Context = collect(ctx)
		if ctx.Err() != nil {
			return
		}
		generator.Generate(ctx, r, func(e llm.Event) {
			// 后台调查仍正常进行，界面只消费最终结果。
			if !e.Done {
				return
			}
			select {
			case ch <- streamMsg{Event: e, Generation: id}:
			case <-ctx.Done():
			}
		})
	}()
	return tea.Batch(wait(ch), m.spinner.Tick)
}
func (m *Model) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.done != nil {
		<-m.done
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.resizeEditor()
		return m, nil
	case spinner.TickMsg:
		if m.phase == "generating" {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(v)
			return m, cmd
		}
		return m, nil
	case streamMsg:
		if v.Generation != m.generation || m.phase != "generating" {
			return m, nil
		}
		e := v.Event
		if !e.Done {
			return m, wait(m.ch)
		}
		if e.Err != nil {
			m.phase = "error"
			m.notice = contextdata.Clean(locale.ErrorText(m.opts.Language, e.Err))
			return m, nil
		}
		if e.Draft == nil {
			m.phase = "error"
			m.notice = locale.Text(m.opts.Language, "No complete command received; nothing executed")
			return m, nil
		}
		d := e.Draft
		if d.Status == "needs_input" {
			m.phase = "clarify"
			m.notice = contextdata.Clean(d.Question)
			m.editor.SetValue("")
			m.editor.Placeholder = ""
			m.resizeEditor()
			return m, m.editor.Focus()
		}
		m.phase = "ready"
		m.editor.SetValue(d.Script)
		m.action = d.SuggestedAction
		if execution.SuggestedAction(d.Script) == "copy" {
			m.action = "copy"
		}
		m.syncAction()
		m.resizeEditor()
		return m, m.editor.Focus()
	case tea.KeyMsg:
		// 粘贴内容中的换行只能作为编辑文本，不能触发确认。
		if !v.Paste {
			switch v.String() {
			case "ctrl+c", "ctrl+d":
				m.Result = Result{}
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		}
		if m.phase == "generating" || m.phase == "pending" {
			return m, nil
		}
		if m.phase == "error" {
			if !v.Paste && v.String() == "enter" {
				return m, m.begin()
			}
			return m, nil
		}
		if !v.Paste {
			switch v.String() {
			case "ctrl+x":
				if m.phase == "ready" {
					if m.action == "execute" {
						m.action = "copy"
					} else {
						m.action = "execute"
					}
					m.syncAction()
				}
				return m, nil
			case "alt+enter", "ctrl+n":
				m.editor.InsertString("\n")
				if m.phase == "ready" {
					m.action = execution.SuggestedAction(m.editor.Value())
					m.syncAction()
				}
				m.resizeEditor()
				return m, nil
			case "enter":
				text := m.editor.Value()
				if m.phase == "input" {
					m.prompt = strings.TrimSpace(text)
					if m.prompt == "" {
						return m, nil
					}
					m.editor.SetValue("")
					return m, m.begin()
				}
				if m.phase == "clarify" {
					if strings.TrimSpace(text) == "" {
						return m, nil
					}
					m.supplement += "\n" + text
					m.editor.SetValue("")
					return m, m.begin()
				}
				if m.phase == "ready" {
					if err := execution.Validate(text); err != nil {
						m.notice = locale.ErrorText(m.opts.Language, err)
						return m, nil
					}
					m.Result = Result{Script: text, Action: m.action}
					if m.cancel != nil {
						m.cancel()
					}
					return m, tea.Quit
				}
				return m, nil
			}
		}
	}
	if m.phase != "input" && m.phase != "ready" && m.phase != "clarify" {
		return m, nil
	}
	old := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if m.editor.Value() != old {
		if m.phase != "clarify" {
			m.notice = ""
		}
		if m.phase == "ready" {
			m.action = execution.SuggestedAction(m.editor.Value())
			m.syncAction()
		}
		m.resizeEditor()
	}
	return m, cmd
}

func (m *Model) fit(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > max(1, m.height) {
		lines = lines[:max(1, m.height)]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, m.width), "…")
	}
	return strings.Join(lines, "\n")
}
func (m *Model) View() string {
	if m.phase == "closed" {
		return ""
	}
	if m.phase == "pending" || m.phase == "generating" {
		return m.fit(m.spinner.View() + " " + locale.Text(m.opts.Language, "loading…"))
	}
	notice := strings.ReplaceAll(contextdata.Clean(m.notice), "\n", " ")
	if m.phase == "error" {
		return m.fit(notice)
	}
	body := m.editor.View()
	if notice != "" {
		body = notice + "\n" + body
	}
	return m.fit(body)
}
