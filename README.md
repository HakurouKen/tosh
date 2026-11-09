# tosh · to-sh

把自然语言变成可编辑的 Shell 命令。模型生成草稿，你确认之后才执行。

## 安装

支持 macOS 和 Linux，运行时无需 Go。项目采用 MIT 许可证，正式源码仓库确定为 [HakurouKen/tosh](https://github.com/HakurouKen/tosh)。当前工作区提供源码构建入口，尚未配置 Release 自动发布、Homebrew tap 或在线安装脚本；下文已确定的发布方案仍待实现。

### 从源码安装（当前可用）

安装 Go 1.25.6 或更高版本，取得项目源码后，在项目根目录执行：

```sh
mkdir -p "$HOME/.local/bin"
go build -trimpath -o "$HOME/.local/bin/tosh" ./cmd/tosh
export PATH="$HOME/.local/bin:$PATH"
tosh -h
```

如需持久生效，把上面的 `export PATH=...` 加入所用 Shell 的配置文件；Fish 用户可改用 `fish_add_path "$HOME/.local/bin"`。安装不需要 `sudo`。也可在项目根目录运行 `go install ./cmd/tosh`，安装目录由 `GOBIN` 决定，未设置时通常为 `$(go env GOPATH)/bin`；需要自行将该目录加入 PATH。

只在源码目录试用时可执行 `make build`，再运行 `./bin/tosh`，无需安装到 PATH。

### 升级与卸载

源码安装的用户取得新版本源码后，重新执行相同构建命令即可升级。通过上述 `~/.local/bin` 方式安装的用户可这样卸载程序：

```sh
rm "$HOME/.local/bin/tosh"
```

配置文件保留在 `${XDG_CONFIG_HOME:-$HOME/.config}/tosh/config.toml`，需要时手动删除。通过 `go install` 安装的用户应删除实际安装目录中的 `tosh`。

### 运行环境

- 使用交互终端及本机的 `sh`、Bash、Zsh 或 Fish；可通过 `--shell` 明确指定。
- 需要可访问的 OpenAI 兼容 API、API Key 和模型名称。
- macOS 使用 `pbcopy` 复制命令；Linux 暂无原生剪贴板实现，不可用时打印命令供手动复制。
- `tmux` 仅用于读取历史终端输出，`lsof` 仅用于按需调查端口，不是基础安装的必需依赖。
- 当前不支持原生 Windows；WSL 可按 Linux 环境使用，但应单独验证。

## 配置

首次使用时创建配置模板：

```sh
tosh --init-config
```

编辑 `~/.config/tosh/config.toml`（设置了 `XDG_CONFIG_HOME` 时使用其下的 `tosh/config.toml`）：

```toml
API_KEY = "your-key"
BASE_URL = "https://api.example.com/v1"
MODEL = "your-model-id"
LANGUAGE = "en" # en（默认）、zh、auto
# 可选：明确指定生成及执行使用的 Shell
# SHELL = "/bin/zsh"
```

支持 OpenAI 兼容 **Chat Completions SSE 流式接口**。BASE_URL 是 API 根地址，只追加 `/chat/completions`，不会自动追加 `/v1`。首版不接入 agent CLI，不要求服务支持 Responses API。服务必须正常结束流并返回完整 JSON；不完整或无效结果不允许执行。支持工具调用时可调查；服务明确拒绝工具参数时自动降级为直接生成。

配置优先级：`--config` 指定文件或默认全局文件 → 非空 `TOSH_API_KEY`、`TOSH_BASE_URL`、`TOSH_MODEL`、`TOSH_SHELL` 按字段覆盖；`--shell` 再覆盖 Shell。配置语法错误始终报错，不读取项目配置。`--init-config` 创建权限为 0600 的模板，拒绝覆盖。配置文件中的密钥不放进模型上下文，不注入执行命令的环境。

## 使用

```sh
tosh "找出当前目录中最大的十个文件"
tosh
tosh --shell /bin/bash "打印当前日期"
tosh --log /tmp/build.log "解释日志并给出修复命令"
tosh --print "打印 hello"  # 仅输出 JSON，永不执行
```

选项放在需求文本前。无需配置 Shell 入口函数。优先从父进程探测 Shell，失败时回退到登录 Shell，并在上下文标注来源；使用 `--shell` 可消除不确定性。

中英文需求均可直接输入。帮助、提示、错误、复制结果及模型说明支持中英文，命令的字面量、路径和指定输出保持原样：

```sh
tosh --lang zh -h
tosh --lang en -h
tosh --lang en "打印原文：你好"
tosh --lang zh "Print hello"
```

语言优先级为 `--lang` → `TOSH_LANG` → 全局配置 `LANGUAGE` → 英文（默认）。只有显式选择 `auto` 时才按系统 locale（`LC_ALL`、`LC_MESSAGES`、`LANG`）选择语言。语言选项放在需求之前；现有全局配置无需修改。文案以英文为 key、中文为 value；没有中文翻译时回退到英文原文。

TUI 在当前终端末尾显示 loading 动画，完成后显示可编辑的备选命令；不清屏、不切换备用屏幕，也不在界面显示操作说明。使用 `tosh -h` 查看快捷键、选项与配置说明。`›` 为执行模式，`⎘` 为复制模式。历史采集与只读调查在后台完成，不再显示日志、上下文、帮助面板或耗时。

| 快捷键 | 行为 |
| --- | --- |
| Enter | 输入需求后生成；确认命令的执行/复制模式；失败时重试 |
| Alt+Enter 或 Ctrl+N | 插入换行 |
| Ctrl+X | 切换执行／复制 |
| Ctrl+C / Ctrl+D | 取消退出，不执行 |

支持 bracketed paste，多行粘贴不会触发确认。必要时显示简洁的错误或澄清问题；澄清时使用同一个输入框补充信息。

确认时先清理本程序的临时编辑区域并恢复终端，保留之前的输出，**原样打印最终确认的命令并换行，再执行同一份文本**。编辑过的命令会按编辑后的内容回显。交互命令直接使用终端；退出码保留，不自动重新执行或修复。命令回显写入失败时不执行。

`cd project && npm test` 可在子 Shell 完成。单独 `cd project`、`export` 等需要改变父 Shell 状态的操作默认复制；可以手动切换。分类为保守提示，不能完整解析任意 Shell。子 Shell 不具有父 Shell 未导出的变量、函数或别名。剪贴板不可用时，退出 TUI 后打印草稿供手动复制，明确提示未执行。

## 历史与调查

每次生成前尝试读取最近 30 条命令，最多读取文件末尾 16 KiB。支持 Zsh 扩展历史、Bash 时间戳、Fish history 格式；优先使用导出的 HISTFILE，默认位置考虑 ZDOTDIR、XDG_DATA_HOME。无集成时无法访问父 Shell 的内存历史，上一条命令可能尚未刷盘；不会启动新交互 Shell 来伪造历史。

历史输出在 tmux 中读取最近 100 行，或通过 `--log` 显式提供日志，最多 16 KiB；其他终端暂不直接读取滚动缓冲区。不自动录制会话，不重跑历史命令。发送前尽力脱敏并清理控制序列，但不能保证识别任意格式的业务机密。

默认一次请求。仅必要时调用受控只读工具：列出项目目录、读取项目内普通文件、查询 TCP 端口监听进程。路径限制在当前目录，排除隐藏及明显凭据/配置路径；没有任意 Shell 工具。最多 3 轮、6 次工具调用；端口查询最多 2 秒。模型返回的最终命令仍需用户确认。

## 第三方分发方案（已确定，待实现）

项目面向无需安装 Go 的普通终端用户，公开源码并以 `HakurouKen/tosh` 的 GitHub Releases 作为主要分发渠道。首期覆盖 macOS/Linux 的 arm64、amd64；Homebrew 在二进制发布稳定后接入，远程 `go install` 作为 Go 开发者的补充入口。

Go CLI 通常有以下分发方式；用户是否安装 Go、是否需要包管理器升级，是选择渠道的主要依据。

| 方式 | 安装者需要 Go | 适用场景 | 本项目安排 |
| --- | --- | --- | --- |
| 下载预编译二进制 | 否 | 普通用户、固定版本部署 | 首期主渠道 |
| Homebrew | 否（使用预编译包时） | macOS 用户安装和升级 | 发布二进制后接入自建 tap |
| `go install 包路径@版本` | 是 | 已有 Go 环境的开发者 | 模块路径规范化后提供 |
| 源码构建 | 是 | 开发、定制、发布前试用 | 当前可用的安装方式 |
| deb/rpm 等系统包 | 否 | Linux 系统管理、企业部署 | 有明确需求后再增加 |
| 安装脚本 | 否（下载预编译包时） | 自动选择系统和架构 | 初期不必维护额外脚本 |

### 首期发布

以版本化 GitHub Release 作为二进制的统一来源，使用 GitHub Actions + GoReleaser，在推送 `v0.x.y` 标签时构建并发布：

- macOS：`darwin_arm64`、`darwin_amd64`。
- Linux：`linux_arm64`、`linux_amd64`。
- 每个平台提供包含 `tosh` 的 `.tar.gz`，例如 `tosh_0.x.y_linux_amd64.tar.gz`，并附 README、MIT 许可证及所需依赖许可声明，以及汇总 `checksums.txt`（SHA-256）。这些是计划中的命名，不是现有下载文件。

当前源码已验证可用 `CGO_ENABLED=0` 构建上述四种目标；交叉编译通过不等于目标平台运行测试通过。发布时还应在 macOS/Linux 上运行测试，并验证打包产物的帮助、版本及基本交互。GoReleaser 的 Go 构建入口需要明确设为 `./cmd/tosh`，不能使用默认根目录入口。

第三方用户的安装流程应是：选择系统和架构 → 下载固定版本压缩包及校验文件 → 校验 SHA-256 → 解压 → 将 `tosh` 安装到 `~/.local/bin` → 检查 PATH → 执行 `tosh --init-config`。Linux 可用 `sha256sum`，macOS 可用 `shasum -a 256` 校验；校验值应与所下载压缩包对应。升级时替换二进制并保留配置，回退时安装旧版压缩包。

不建议首期以 Docker 作为主要安装方式：tosh 需要访问用户当前工作目录、历史记录和交互 Shell；容器需要额外挂载目录并处理终端、Shell 和权限映射。

### 发布前尚需完成

1. 源码以 MIT 许可证托管于 `HakurouKen/tosh`；发布二进制前核对并补齐所需依赖许可声明。当前尚无发布工作流或正式二进制 Release。
2. 提供远程 `go install` 前，将 `module tosh` 改为 `github.com/HakurouKen/tosh`，并同步修改内部导入。发布版本后可提供 `go install github.com/HakurouKen/tosh/cmd/tosh@latest`，也可用实际版本标签替换 `latest`；当前源码尚未完成该调整，此命令暂不作为可用安装入口。二进制发布本身不以此修改为前提。
3. 将 `--version` 当前硬编码的 `tosh 0.1.0` 改为可由发布构建注入的版本信息，确保与 Release 标签一致。
4. 增加 GoReleaser 配置及标签触发的 CI，完成四种产物打包、校验文件生成和 Release 上传。发布包不包含开发者配置或 API Key。
5. 二进制发布稳定后接入 Homebrew tap。需另行确定配方形式、macOS 签名与公证安排；Linux 仍保留直接下载入口，不假定 macOS 的 Cask 可用于 Linux。

正式仓库地址已确定，但发布渠道尚未在本工作区实现，因此本文暂不提供具体版本的下载命令、`brew install` 命令或 `curl | sh` 入口。

参考：[Go 的 go install 规则](https://go.dev/ref/mod#go-install)、[GoReleaser 快速开始](https://goreleaser.com/getting-started/quick-start/)、[Go 构建配置](https://goreleaser.com/customization/builds/builders/go/)、[Homebrew Cask 发布](https://goreleaser.com/customization/publish/homebrew_casks/)。

## 开发验证

```sh
make test          # go test -count=1 -race ./...，含本地 PTY e2e
make test-short    # 快速验证，跳过 PTY e2e
make test-e2e      # 仅 CLI/PTY e2e，可重复执行、不使用测试缓存
make vet
make build
```

测试集中在 `tests/e2e/`，不再依赖 Python 或预构建的 `bin/tosh`。每个测试进程从当前源码构建一次临时二进制，使用独立工作目录、示例 history、配置和环境；失败及超时后回收子进程和 PTY。macOS/Linux 使用 `/bin/sh` 测试，其他平台明确跳过。

`go test ./...` 默认包含本地模拟 API 的 PTY e2e；`go test -short ./...` 跳过 PTY，但仍验证 CLI 帮助和语言优先级。PTY 覆盖最终命令回显后执行、多行粘贴、缩放后继续编辑与取消、信号恢复、复制降级、交互 stdin、退出码及 history 请求载荷。可用 `go test -count=1 -v ./tests/e2e -run '^TestPTY/InteractiveStdin$'` 单独重跑一个场景。单元测试继续覆盖配置、history 解析和缺失、密钥遮蔽、流中断、错误输出、工具调用及文件路径限制。

真实服务与性能测试始终需要显式启用，不会因为环境中已有密钥而自动运行：

```sh
# 先在环境中设置 TOSH_API_KEY、TOSH_BASE_URL、TOSH_MODEL
make test-live
TOSH_LIVE_INVESTIGATE=1 make test-live
make test-startup
```

live 冒烟测试只验证真实 API → TUI → 编辑确认 → 执行的链路，会把模型草稿替换成固定的无害打印命令。它不验证模型命令正确性；调查开关只增加调查请求，不断言工具一定被调用。调查使用临时目录中的示例 README，不读取项目目录。显式启用却缺少必需配置时测试失败，不保存凭据。

启动性能单独预热一次，再采集 25 次从启动进程到观察到 loading 文案的时间，报告中位数和 P95，并检查 P95 ≤ 200ms。该结果受运行机器负载影响，不作为默认功能回归的门槛。

## 许可证

本项目采用 [MIT 许可证](LICENSE)。第三方依赖保留各自的许可证及版权声明。
