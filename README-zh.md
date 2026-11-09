# tosh · to-sh

[English](README.md) | 简体中文

把自然语言变成可编辑的 Shell 命令。模型生成命令草稿，你编辑、确认后才执行，也可以选择只复制。

支持 macOS 和 Linux，以及本机的 sh、Bash、Zsh、Fish。运行时无需 Go，需要交互终端及 OpenAI 兼容的模型服务。

## 安装

### GitHub Releases

无需安装 Go。前往 [Releases](https://github.com/HakurouKen/tosh/releases)，下载与你的系统（macOS / Linux）和架构（arm64 / amd64）对应的安装包。

解压后，在包含 `tosh` 的目录执行：

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 tosh "$HOME/.local/bin/tosh"
```

### 从源码安装

需要 Go 1.25.6 或更高版本：

```sh
git clone https://github.com/HakurouKen/tosh.git
cd tosh
mkdir -p "$HOME/.local/bin"
make build
install -m 755 bin/tosh "$HOME/.local/bin/tosh"
```

### 设置 PATH

两种安装方式均需将安装目录加入 PATH：

```sh
export PATH="$HOME/.local/bin:$PATH"
tosh -h
```

将 `export PATH=...` 加入所用 Shell 的配置文件以持久生效；Fish 用户可改用 `fish_add_path "$HOME/.local/bin"`。

升级时安装新版 Release，或取得最新源码重新构建；卸载时删除 `~/.local/bin/tosh`。

## 配置

```sh
tosh --init-config
```

编辑 `~/.config/tosh/config.toml`；设置了 `XDG_CONFIG_HOME` 时，路径为其下的 `tosh/config.toml`：

```toml
API_KEY = "your-key"
BASE_URL = "https://api.example.com/v1"
MODEL = "your-model-id"
LANGUAGE = "en" # en（默认）、zh、auto
# SHELL = "/bin/zsh" # 可选，明确指定 Shell
```

模型服务须支持 **Chat Completions SSE 流式接口**。`BASE_URL` 只追加 `/chat/completions`，不会自动追加 `/v1`。

可用 `--config` 指定配置文件，或用 `TOSH_API_KEY`、`TOSH_BASE_URL`、`TOSH_MODEL`、`TOSH_SHELL` 环境变量覆盖对应字段。

界面及模型说明的语言优先级：`--lang` → `TOSH_LANG` → `LANGUAGE` → 英文。显式选择 `auto` 时才按系统 locale 选择语言；需求可使用中英文。

## 使用

```sh
tosh "找出当前目录中最大的十个文件"
tosh                                      # 交互输入需求
tosh --shell /bin/bash "打印当前日期"
tosh --log /tmp/build.log "解释日志并给出修复命令"
tosh --print "打印 hello"                  # 仅输出 JSON，不执行
tosh --lang zh -h                          # 中文帮助
```

选项放在需求文本之前，完整说明见 `tosh -h`。默认探测当前 Shell，也可用 `--shell` 指定。

命令草稿可直接编辑。`›` 表示执行模式，`⎘` 表示复制模式。

| 快捷键 | 行为 |
| --- | --- |
| Enter | 提交需求；按当前模式确认命令；失败时重试 |
| Alt+Enter / Ctrl+N | 插入换行 |
| Ctrl+X | 切换执行／复制模式 |
| Ctrl+C / Ctrl+D | 取消退出，不执行 |

多行粘贴不会自动确认。执行前回显最终确认的命令，执行后保留退出码。

单独 `cd`、`export` 等改变父 Shell 状态的操作默认进入复制模式。macOS 使用 `pbcopy`；Linux 或剪贴板不可用时，打印命令供手动复制。

## 开发

```sh
make build        # 构建到 bin/tosh
make test         # 运行测试，含 race 检测
make test-short   # 快速验证
make vet          # 静态检查
```

使用 `./bin/tosh` 运行本地构建。默认测试无需 API Key。

版本和发布使用 `make version`、`make bump PART=patch`、`make tag`、`make release`，详见[发布指南](docs/releasing.md)。

## 许可证

[MIT](LICENSE)。第三方依赖保留各自的许可证及版权声明。
