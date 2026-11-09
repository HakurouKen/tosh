package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"tosh/internal/contextdata"
)

func toolDefinitions() []any {
	def := func(name, desc string, props map[string]any, required []string) any {
		return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": desc, "parameters": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}}}
	}
	return []any{
		def("list_directory", "只读列出当前项目内目录的直接子项，不递归", map[string]any{"path": map[string]string{"type": "string"}}, []string{"path"}),
		def("read_file", "只读读取当前项目内普通文本文件，最多 16 KiB；不可读取凭据或配置文件", map[string]any{"path": map[string]string{"type": "string"}}, []string{"path"}),
		def("port_process", "只读查询 TCP 端口监听进程，不终止进程", map[string]any{"port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}}, []string{"port"}),
	}
}
func safePath(root, p string) (string, error) {
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径超出项目范围")
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		l := strings.ToLower(part)
		if strings.HasPrefix(l, ".") || strings.Contains(l, "secret") || strings.Contains(l, "credential") || strings.Contains(l, "config") || strings.HasSuffix(l, ".pem") || strings.HasSuffix(l, ".key") {
			return "", fmt.Errorf("不读取可能包含凭据的路径")
		}
	}
	return p, nil
}
func RunTool(ctx context.Context, root, name, args string) string {
	if ctx.Err() != nil {
		return "调查已取消"
	}
	if name == "port_process" {
		var a struct {
			Port int `json:"port"`
		}
		d := json.NewDecoder(strings.NewReader(args))
		d.DisallowUnknownFields()
		if d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF || a.Port < 1 || a.Port > 65535 {
			return "端口参数无效"
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, "lsof", "-nP", "-iTCP:"+strconv.Itoa(a.Port), "-sTCP:LISTEN")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = 100 * time.Millisecond
		out := &boundedOutput{}
		cmd.Stdout = out
		cmd.Stderr = out
		e := cmd.Run()
		if c.Err() != nil {
			return "端口查询超时或已取消"
		}
		if e != nil && out.s.Len() == 0 {
			return "未发现监听进程，或 lsof 不可用"
		}
		return contextdata.Redact(out.s.String())
	}
	if name != "list_directory" && name != "read_file" {
		return "不支持此调查工具"
	}
	var a struct {
		Path string `json:"path"`
	}
	d := json.NewDecoder(strings.NewReader(args))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil || d.Decode(new(any)) != io.EOF || a.Path == "" {
		return "路径参数无效"
	}
	p, e := safePath(root, a.Path)
	if e != nil {
		return "路径不可访问或不允许读取"
	}
	rootPath, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "项目目录不可访问"
	}
	rel, e := filepath.Rel(rootPath, p)
	if e != nil {
		return "路径不可访问"
	}
	rootFS, e := os.OpenRoot(rootPath)
	if e != nil {
		return "项目目录不可访问"
	}
	defer rootFS.Close()
	// os.Root 在实际打开时限制路径，避免检查与读取之间的符号链接竞态。
	f, e := rootFS.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return "路径不可访问"
	}
	defer f.Close()
	if name == "read_file" {
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() {
			return "无法读取普通文件"
		}
		t := st.Size() > 16384
		if t {
			if _, e = f.Seek(-16384, io.SeekEnd); e != nil {
				return "无法读取文件"
			}
		}
		b, e := io.ReadAll(io.LimitReader(f, 16384))
		if e != nil {
			return "无法读取文件"
		}
		if strings.ContainsRune(string(b), 0) {
			return "不读取二进制文件"
		}
		out := contextdata.Redact(string(b))
		if t {
			out += "\n（仅保留文件尾部 16 KiB）"
		}
		return out
	}
	entries, e := f.ReadDir(100)
	if e != nil && len(entries) == 0 {
		return "无法列目录"
	}
	var b strings.Builder
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		b.WriteString(entry.Name())
		if entry.IsDir() {
			b.WriteString("/")
		}
		b.WriteByte('\n')
	}
	if len(entries) == 100 {
		b.WriteString("（最多显示 100 项）")
	}
	return contextdata.Redact(b.String())
}

type boundedOutput struct{ s strings.Builder }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remain := 16384 - b.s.Len()
	if remain > 0 {
		if len(p) > remain {
			p = p[:remain]
		}
		b.s.Write(p)
	}
	return n, nil
}
