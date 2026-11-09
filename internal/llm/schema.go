package llm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"tosh/internal/locale"
)

// DraftSchema 描述最终结果协议。兼容服务无需实现服务端结构化输出，
// 本地主动执行同等形状校验，并在 ParseDraft 中检查语义及更严格的字节上限。
//
//go:embed draft.schema.json
var DraftSchema string

var draftShape = func() struct {
	Required   []string `json:"required"`
	Properties map[string]struct {
		Type string `json:"type"`
	} `json:"properties"`
} {
	var s struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(DraftSchema), &s); err != nil {
		panic("invalid embedded draft schema")
	}
	return s
}()

func validateShape(fields map[string]json.RawMessage) error {
	for _, name := range draftShape.Required {
		if _, ok := fields[name]; !ok {
			return locale.Fail("Result is missing %s; nothing executed", name)
		}
	}
	for name, raw := range fields {
		rule, ok := draftShape.Properties[name]
		if !ok {
			return locale.Fail("Result contains unknown fields; nothing executed")
		}
		raw = bytes.TrimSpace(raw)
		switch rule.Type {
		case "string":
			if len(raw) == 0 || raw[0] != '"' {
				return locale.Fail("Invalid result field type; nothing executed")
			}
		case "array":
			if len(raw) == 0 || raw[0] != '[' {
				return locale.Fail("Invalid result field type; nothing executed")
			}
			var items []json.RawMessage
			if json.Unmarshal(raw, &items) != nil {
				return locale.Fail("Invalid result array; nothing executed")
			}
			for _, item := range items {
				item = bytes.TrimSpace(item)
				if len(item) == 0 || item[0] != '"' {
					return locale.Fail("Invalid result array item; nothing executed")
				}
			}
		}
	}
	return nil
}
