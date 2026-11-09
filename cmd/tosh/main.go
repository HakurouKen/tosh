package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
	"tosh"
	"tosh/internal/config"
	"tosh/internal/contextdata"
	"tosh/internal/execution"
	"tosh/internal/llm"
	"tosh/internal/locale"
	"tosh/internal/ui"
)

func main() { os.Exit(run()) }
func run() int {
	initialLanguage, initialPath := bootstrapOptions(os.Args[1:])
	if initialLanguage == "" {
		initialLanguage = config.LanguagePreference(initialPath)
	}
	lang, languageError := locale.Resolve(initialLanguage)
	if languageError != nil {
		fmt.Fprintln(os.Stderr, locale.ErrorText(lang, languageError))
		return 2
	}
	tr := func(source string) string { return locale.Text(lang, source) }
	report := func(err error) { fmt.Fprintln(os.Stderr, locale.ErrorText(lang, err)) }
	fs := flag.NewFlagSet("tosh", flag.ContinueOnError)
	path := fs.String("config", "", tr("Global configuration file"))
	shellFlag := fs.String("shell", "", tr("Target shell path or name"))
	logPath := fs.String("log", "", tr("Include the tail of a log file"))
	printOnly := fs.Bool("print", false, tr("Generate JSON only; do not open the TUI or execute"))
	initConfig := fs.Bool("init-config", false, tr("Create a global config template without overwriting"))
	version := fs.Bool("version", false, tr("Show version"))
	langFlag := fs.String("lang", "", tr("UI and model response language (auto, zh, en)"))
	fs.Usage = func() {
		helpPath := *path
		if helpPath == "" {
			helpPath = initialPath
		}
		if helpPath == "" {
			helpPath = config.DefaultPath()
		}
		fmt.Fprintln(fs.Output(), locale.Help(lang, helpPath))
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *version {
		fmt.Println("tosh 0.1.0")
		return 0
	}
	if *initConfig {
		if e := config.Init(*path); e != nil {
			report(e)
			return 1
		}
		fmt.Println(tr("Config template created. Set API_KEY, BASE_URL, and MODEL."))
		return 0
	}
	cfg, e := config.Load(*path)
	if e != nil {
		report(e)
		return 1
	}
	language := cfg.Language
	if *langFlag != "" {
		language = *langFlag
	}
	lang, e = locale.Resolve(language)
	if e != nil {
		report(e)
		return 2
	}
	cfg.Language = string(lang)
	explicit := cfg.Shell
	if *shellFlag != "" {
		explicit = *shellFlag
	}
	shell, source := contextdata.DetectShell(explicit)
	switch filepath.Base(shell) {
	case "zsh", "bash", "fish", "sh":
	default:
		fmt.Fprintln(os.Stderr, tr("Supported shells: zsh, bash, fish, sh. Select one with --shell"))
		return 1
	}
	prompt := strings.Join(fs.Args(), " ")
	client := &llm.Client{Config: cfg, SystemPrompt: tosh.SystemPrompt()}
	collect := func(ctx context.Context) contextdata.Snapshot {
		s := contextdata.Collect(ctx, shell, source, *logPath)
		if cfg.APIKey != "" {
			s.History = redactHistory(s.History, cfg.APIKey)
			s.Output = strings.ReplaceAll(s.Output, cfg.APIKey, "[REDACTED]")
		}
		return s
	}
	if *printOnly {
		if prompt == "" {
			fmt.Fprintln(os.Stderr, tr("--print requires a prompt"))
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		var draft *llm.Draft
		var failure error
		client.Generate(ctx, llm.Request{Prompt: prompt, Context: collect(ctx)}, func(e llm.Event) {
			if e.Draft != nil {
				draft = e.Draft
			}
			if e.Err != nil {
				failure = e.Err
			}
		})
		if failure != nil {
			report(failure)
			return 1
		}
		if draft == nil {
			fmt.Fprintln(os.Stderr, tr("Request canceled or no draft produced"))
			return 1
		}
		json.NewEncoder(os.Stdout).Encode(draft)
		return 0
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, tr("Interactive mode requires a terminal; use --print for generation only"))
		return 2
	}
	m := ui.New(ui.Options{Prompt: prompt, Generator: client, Collect: collect, Language: lang})
	p := tea.NewProgram(m, tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.QuitMsg); ok {
			m.Dismiss()
		}
		return msg
	}))
	_, e = p.Run()
	m.Stop()
	if e != nil {
		fmt.Fprintln(os.Stderr, tr("TUI exited:"), locale.ErrorText(lang, e))
		return 1
	}
	result := m.Result
	if result.Script == "" {
		return 0
	}
	if result.Action == "copy" {
		e = execution.Copy(result.Script, os.Stdout, lang)
	} else if result.Action == "execute" {
		e = execution.Execute(shell, result.Script)
	} else {
		fmt.Fprintln(os.Stderr, tr("Invalid action; nothing executed"))
		return 1
	}
	if e != nil {
		report(e)
		return 1
	}
	return 0
}

// 提前选择帮助语言；与 flag 一样，在首个位置参数或 -- 处停止。
func bootstrapOptions(args []string) (language, path string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			break
		}
		name, value, assigned := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch name {
		case "lang", "config", "shell", "log":
			if !assigned {
				if i+1 >= len(args) {
					continue
				}
				i++
				value = args[i]
			}
			if name == "lang" {
				language = value
			}
			if name == "config" {
				path = value
			}
		}
	}
	return
}
func redactHistory(history []string, key string) []string {
	for i := range history {
		history[i] = strings.ReplaceAll(history[i], key, "[REDACTED]")
	}
	return history
}
