# tosh · to-sh

English | [简体中文](README-zh.md)

Turn natural language into editable shell commands. Review and edit the draft before confirming execution, or choose to copy it instead.

Supports macOS and Linux with a local sh, Bash, Zsh, or Fish shell. Go is not required at runtime. You need an interactive terminal and an OpenAI-compatible model service.

## Installation

### GitHub Releases

No Go installation required. Download the archive for your system (macOS / Linux) and architecture (arm64 / amd64) from [Releases](https://github.com/HakurouKen/tosh/releases).

Extract the archive, then run these commands from the directory containing `tosh`:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 tosh "$HOME/.local/bin/tosh"
```

### Build from source

Requires Go 1.25.6 or later:

```sh
git clone https://github.com/HakurouKen/tosh.git
cd tosh
mkdir -p "$HOME/.local/bin"
make build
install -m 755 bin/tosh "$HOME/.local/bin/tosh"
```

### Set up PATH

For either installation method, add the installation directory to PATH:

```sh
export PATH="$HOME/.local/bin:$PATH"
tosh -h
```

Add the `export PATH=...` line to your shell configuration to make it permanent. Fish users can use `fish_add_path "$HOME/.local/bin"` instead.

To upgrade, install a newer Release or rebuild from the latest source. To uninstall, delete `~/.local/bin/tosh`.

## Configuration

```sh
tosh --init-config
```

Edit `~/.config/tosh/config.toml`. If `XDG_CONFIG_HOME` is set, the file is `tosh/config.toml` under that directory:

```toml
API_KEY = "your-key"
BASE_URL = "https://api.example.com/v1"
MODEL = "your-model-id"
LANGUAGE = "en" # en (default), zh, auto
# SHELL = "/bin/zsh" # Optional: explicitly select a shell
```

The model service must support **Chat Completions with SSE streaming**. Only `/chat/completions` is appended to `BASE_URL`; `/v1` is not added automatically.

Use `--config` to select a configuration file, or override individual fields with `TOSH_API_KEY`, `TOSH_BASE_URL`, `TOSH_MODEL`, and `TOSH_SHELL` environment variables.

The language for the interface and model explanations follows this precedence: `--lang` → `TOSH_LANG` → `LANGUAGE` → English. The system locale is used only when `auto` is explicitly selected. Prompts can be in English or Chinese.

## Usage

```sh
tosh "Find the ten largest files in the current directory"
tosh                                      # Enter a prompt interactively
tosh --shell /bin/bash "Print the current date"
tosh --log /tmp/build.log "Explain this log and suggest a fix"
tosh --print "Print hello"                 # Output JSON only; do not execute
tosh --lang zh -h                          # Show help in Chinese
```

Place options before the prompt. See `tosh -h` for full details. The current shell is detected automatically; use `--shell` to select one explicitly.

Edit the command draft directly. `›` indicates execution mode, and `⎘` indicates copy mode.

| Shortcut | Action |
| --- | --- |
| Enter | Submit a prompt; confirm the command in the current mode; retry after an error |
| Alt+Enter / Ctrl+N | Insert a newline |
| Ctrl+X | Switch between execution and copy modes |
| Ctrl+C / Ctrl+D | Cancel and exit without executing |

Pasting multiple lines does not confirm automatically. The final command is printed before execution, and its exit code is preserved.

Commands that change the parent shell state, such as standalone `cd` or `export`, default to copy mode. macOS uses `pbcopy`; on Linux or when the clipboard is unavailable, the command is printed for manual copying.

## Development

```sh
make build        # Build bin/tosh
make test         # Run tests, including race detection
make test-short   # Run a quick check
make vet          # Run static checks
```

Run the local build with `./bin/tosh`. Default tests do not require an API key.

For versioning and releases, use `make version`, `make bump PART=patch`, `make tag`, and `make release`. See the [release guide (Chinese)](docs/releasing.md).

## License

[MIT](LICENSE). Third-party dependencies retain their own licenses and copyright notices.
