package locale

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Language string

const (
	Chinese Language = "zh"
	English Language = "en"
)

// Resolve 仅对显式设置报错；系统的其他语言统一回退到英文。
func Resolve(value string) (Language, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return English, nil
	}
	if value == "auto" {
		for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := os.Getenv(key); v != "" {
				if strings.HasPrefix(strings.ToLower(v), "zh") {
					return Chinese, nil
				}
				return English, nil
			}
		}
		return English, nil
	}
	if value == "zh" || strings.HasPrefix(value, "zh-") || strings.HasPrefix(value, "zh_") {
		return Chinese, nil
	}
	if value == "en" || strings.HasPrefix(value, "en-") || strings.HasPrefix(value, "en_") {
		return English, nil
	}
	return English, Fail("Unsupported language: %s (choose auto, zh, or en)", value)
}

func Text(lang Language, source string, args ...any) string {
	if lang == Chinese {
		if translated, ok := chinese[source]; ok {
			source = translated
		}
	}
	if len(args) == 0 {
		return source
	}
	return fmt.Sprintf(source, args...)
}

type messageError struct {
	source string
	args   []any
}

func (e *messageError) Error() string       { return fmt.Sprintf(e.source, e.args...) }
func Fail(source string, args ...any) error { return &messageError{source: source, args: args} }
func ErrorText(lang Language, err error) string {
	if err == nil {
		return ""
	}
	var msg *messageError
	if errors.As(err, &msg) {
		return Text(lang, msg.source, msg.args...)
	}
	return Text(lang, err.Error())
}

func ModelLanguage(lang Language) string {
	if lang == Chinese {
		return "Simplified Chinese"
	}
	return "English"
}

var chinese = map[string]string{
	"loading…": "加载中…",
	"Unsupported language: %s (choose auto, zh, or en)":   "不支持的语言：%s（可选 auto、zh、en）",
	"Global configuration file":                           "全局配置路径",
	"Target shell path or name":                           "目标 Shell 路径或名称",
	"Include the tail of a log file":                      "附带指定日志的尾部内容",
	"Generate JSON only; do not open the TUI or execute":  "仅生成 JSON，不进入 TUI、不执行命令",
	"Create a global config template without overwriting": "创建全局配置模板，不覆盖已有文件",
	"Show version": "显示版本",
	"UI and model response language (auto, zh, en)":                                "界面及模型说明语言（auto、zh、en）",
	"Config template created. Set API_KEY, BASE_URL, and MODEL.":                   "配置模板已创建，请填写 API_KEY、BASE_URL、MODEL。",
	"Invalid TOML configuration: %s":                                               "配置 TOML 语法错误：%s",
	"Cannot read configuration: %s":                                                "无法读取配置：%s",
	"Set API_KEY, BASE_URL, and MODEL in %s, or use TOSH_ environment variables":   "请在 %s 配置 API_KEY、BASE_URL、MODEL，或设置 TOSH_ 前缀环境变量",
	"BASE_URL must be an HTTP(S) API root without credentials, query, or fragment": "BASE_URL 必须是不含凭据、查询或片段的 HTTP(S) API 根地址",
	"Configuration already exists or cannot be created: %s":                        "配置已存在或无法创建：%s",
	"Supported shells: zsh, bash, fish, sh. Select one with --shell":               "目标 Shell 仅支持 zsh、bash、fish、sh；请使用 --shell 指定",
	"--print requires a prompt":                                                    "--print 需要提供需求",
	"Request canceled or no draft produced":                                        "请求已取消或未产生草稿",
	"Interactive mode requires a terminal; use --print for generation only":        "交互模式需要终端；仅生成可使用 --print",
	"TUI exited:":                                                                    "TUI 已退出：",
	"Invalid action; nothing executed":                                               "无效执行模式，未执行",
	"No complete command received; nothing executed":                                 "未获得完整命令，尚未执行",
	"Command is empty or too long":                                                   "命令为空或过长",
	"Command contains control characters; nothing executed":                          "命令含控制字符，未执行",
	"Target shell is unavailable":                                                    "目标 Shell 不可用",
	"Cannot print the command; nothing executed":                                     "无法输出待执行命令，未执行",
	"Clipboard unavailable; copy this command manually. It has not been executed:":   "复制不可用；以下命令尚未执行，请手动复制：",
	"Copied, not executed. Paste into your current shell to run.":                    "已复制，尚未执行。请粘贴到当前 Shell 执行。",
	"Model returned invalid JSON; nothing executed":                                  "模型未返回有效 JSON；未执行",
	"Result is missing %s; nothing executed":                                         "结果缺少 %s；未执行",
	"Result contains unknown fields; nothing executed":                               "结果包含未知字段；未执行",
	"Invalid result field type; nothing executed":                                    "结果字段类型无效；未执行",
	"Invalid result array; nothing executed":                                         "结果数组无效；未执行",
	"Invalid result array item; nothing executed":                                    "结果数组元素无效；未执行",
	"Invalid model result fields; nothing executed":                                  "模型结果字段无效；未执行",
	"Model result exceeds limits or targets a different shell; nothing executed":     "模型结果超出约定或目标 Shell 不一致；未执行",
	"Command contains terminal control characters; nothing executed":                 "命令包含终端控制字符；未执行",
	"Command draft is incomplete; nothing executed":                                  "命令草稿不完整；未执行",
	"Invalid clarification result; nothing executed":                                 "澄清结果无效；未执行",
	"Unknown result status; nothing executed":                                        "未知结果状态；未执行",
	"Result notes are too long; nothing executed":                                    "说明过长；未执行",
	"Internal generation error; nothing executed":                                    "生成过程中出现内部错误；未执行",
	"Model output did not finish normally; nothing executed":                         "模型输出未正常完成；未执行",
	"Investigation budget exceeded or protocol incomplete; nothing executed":         "调查超出预算或协议不完整；未执行",
	"Invalid tool call identifier; nothing executed":                                 "工具调用标识无效；未执行",
	"Investigation limit reached; nothing executed":                                  "调查次数已用完；未执行",
	"Cannot create the API request":                                                  "无法构建 API 请求",
	"API connection failed or timed out; check BASE_URL and your proxy":              "API 连接失败或超时，请检查 BASE_URL 和代理",
	"API returned HTTP %d; check the model, credentials, and protocol compatibility": "API 返回 HTTP %d，请检查模型、认证及协议兼容性",
	"API did not return SSE; only streaming Chat Completions is supported":           "API 未返回 SSE 流；当前仅支持流式 Chat Completions",
	"Invalid API event; nothing executed":                                            "API 事件无效；未执行",
	"Content received after the finish event; nothing executed":                      "结束事件后仍有内容；未执行",
	"Model output exceeds the size limit":                                            "模型输出超出大小限制",
	"Too many tool calls":                                                            "工具数量超限",
	"Tool arguments exceed the size limit":                                           "工具参数超限",
	"API stream interrupted; nothing executed":                                       "API 流中断；未执行",
	"API stream is incomplete; nothing executed":                                     "API 流不完整；未执行",
	helpEnglish: helpChinese,
}

func Help(lang Language, path string) string {
	return Text(lang, helpEnglish, path)
}

const helpEnglish = `Usage: tosh [options] [prompt]

Append loading and an editable command to the current terminal without clearing it.
Enter confirms execution; the ⎘ prefix means copy only, without executing.
Ctrl+X switches execute/copy; Alt+Enter or Ctrl+N inserts a newline; Ctrl+C/Ctrl+D cancels.
After an error, Enter retries. Answer clarification questions in the same editor.
On confirmation, leave the editor, print the final command and a newline, then execute.
Prompts may be Chinese or English; command literals and paths are preserved.

Global config: %s
Fields: API_KEY, BASE_URL, MODEL; optional SHELL and LANGUAGE (auto, zh, en).
Nonempty TOSH_API_KEY, TOSH_BASE_URL, TOSH_MODEL, TOSH_SHELL, TOSH_LANG override the file.
Language precedence: --lang > TOSH_LANG > LANGUAGE > English (default).
Use auto to detect LC_ALL, LC_MESSAGES, then LANG.

Options:`

const helpChinese = `用法：tosh [选项] [需求]

在当前终端追加显示加载动画和可编辑命令，不清屏。
Enter 确认执行；⎘ 前缀表示确认后复制，不执行。
Ctrl+X 切换执行/复制；Alt+Enter 或 Ctrl+N 换行；Ctrl+C/Ctrl+D 取消。
生成失败时 Enter 重试；需要澄清时直接输入补充信息。
确认后退出编辑，先回显最终命令并换行，再执行。
支持中文和英文需求，命令中的字面量与路径保持原样。

全局配置：%s
字段：API_KEY、BASE_URL、MODEL，可选 SHELL、LANGUAGE（auto、zh、en）。
非空 TOSH_API_KEY、TOSH_BASE_URL、TOSH_MODEL、TOSH_SHELL、TOSH_LANG 覆盖文件配置。
语言优先级：--lang > TOSH_LANG > LANGUAGE > 英文（默认）。
显式选择 auto 时，依次检测 LC_ALL、LC_MESSAGES、LANG。

选项：`
