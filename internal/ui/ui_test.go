package ui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"tosh/internal/llm"
	"tosh/internal/locale"
)

type fakeGenerator struct{}

func TestMinimalViews(t *testing.T) {
	m := readyModel()
	m.phase = "generating"
	view := m.View()
	if !strings.Contains(view, "loading") || strings.Contains(view, "printf") || strings.Count(view, "\n") != 0 {
		t.Fatalf("loading view is not minimal: %q", view)
	}
	m.opts.Language = locale.Chinese
	if view := m.View(); !strings.Contains(view, "加载中…") || strings.Contains(view, "loading") {
		t.Fatalf("Chinese loading view not localized: %q", view)
	}
	m.phase = "ready"
	m.prompt = "hidden requirement"
	view = m.View()
	for _, hidden := range []string{"hidden requirement", "history", "首事件", "补充要求", "Ctrl+", "Enter", "F1", "tosh ·"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("unexpected panel: %s", hidden)
		}
	}
	if !strings.Contains(view, "printf original") {
		t.Fatal("candidate command missing")
	}
}

func TestNarrowViewPreservesActionAndDoesNotOverflow(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 30, Height: 12}, {Width: 45, Height: 15}, {Width: 80, Height: 24}} {
		m := readyModel()
		m.Update(size)
		view := m.View()
		if !strings.Contains(view, "printf original") {
			t.Fatal("candidate command hidden")
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size.Height {
			t.Fatal("vertical overflow")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size.Width {
				t.Fatal("horizontal overflow")
			}
		}
	}
}

func TestCopyModeUsesOnlyPromptSymbol(t *testing.T) {
	m := readyModel()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.action != "copy" || !strings.Contains(m.View(), "⎘") {
		t.Fatal("copy mode is not indicated")
	}
	if strings.Contains(m.View(), "Enter") || strings.Contains(m.View(), "Ctrl+") {
		t.Fatal("inline help must live in -h")
	}
	m.Dismiss()
	if m.View() != "" {
		t.Fatal("temporary edit area remains after quit")
	}
}

func TestMultilineKeyDoesNotExecute(t *testing.T) {
	m := readyModel()
	m.editor.SetValue("cd /tmp")
	m.action = "copy"
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.Result.Script != "" {
		t.Fatal("newline executed")
	}
}

func (fakeGenerator) Generate(context.Context, llm.Request, func(llm.Event)) {}
func readyModel() *Model {
	m := New(Options{Generator: fakeGenerator{}})
	m.phase = "ready"
	m.editor.SetValue("printf original")
	m.action = "execute"
	return m
}
func TestConfirmationFreezesEditedText(t *testing.T) {
	m := readyModel()
	m.editor.SetValue("printf 'edited value'")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.Result.Script != "printf 'edited value'" {
		t.Fatal("wrong script confirmed")
	}
}
func TestPasteDoesNotExecute(t *testing.T) {
	m := readyModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\nprintf pasted\n"), Paste: true})
	if m.Result.Script != "" {
		t.Fatal("paste executed")
	}
	if m.editor.Value() == "printf original" {
		t.Fatal("paste not inserted")
	}
}
func TestCancelNeverExecutesAndLateEventsIgnored(t *testing.T) {
	m := readyModel()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.Result.Script != "" {
		t.Fatal("cancel executed")
	}
	m.phase = "generating"
	m.generation = 2
	m.Update(streamMsg{Generation: 1, Event: llm.Event{Done: true, Draft: &llm.Draft{Status: "ready", Script: "bad"}}})
	if m.phase != "generating" {
		t.Fatal("late event accepted")
	}
}
func TestFailureCannotConfirmOldDraft(t *testing.T) {
	m := readyModel()
	m.phase = "error"
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	if m.Result.Script != "" {
		t.Fatal("old draft executed after failure")
	}
}
func TestEditReclassifiesAction(t *testing.T) {
	m := readyModel()
	m.editor.SetValue("")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cd /tmp"), Paste: true})
	if m.action != "copy" {
		t.Fatal("cd not reclassified")
	}
}
