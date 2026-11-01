// Package xshell 解析 Xshell 会话文件（.xsh）并转换为可导入的主机预览。
// 仅提取非敏感字段：主机/端口/用户名/认证方式。Xshell 的密码是本机加密
// 存储、物理上不可解密迁移；其密钥（.xpk）为专有加密格式亦不可直接使用，
// 导入后需用户补录凭据（见 README 与导入界面说明）。
// 本包零内部依赖（依赖方向见 ARCHITECTURE.md）。
package xshell

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Preview 单个 Xshell 会话的导入预览
type Preview struct {
	File       string `json:"file"`                 // 源文件名（xxx.xsh）
	Name       string `json:"name"`                 // 会话名（文件名去后缀）
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`             // password | key | keyboard_interactive
	KeyName    string `json:"keyName,omitempty"`    // Xshell 密钥名（仅提示用，密钥本体不可迁移）
	Valid      bool   `json:"valid"`
	SkipReason string `json:"skipReason,omitempty"` // Valid=false 时的原因
}

// ScanDir 扫描目录下的全部 .xsh 文件（不含子目录），返回按名称排序的预览列表。
// 解析失败的文件以 Valid=false + SkipReason 呈现，不中断整体导入。
func ScanDir(dir string) ([]Preview, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	var out []Preview
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".xsh") {
			continue
		}
		out = append(out, parseFile(filepath.Join(dir, e.Name())))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseFile(path string) Preview {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	p := Preview{File: filepath.Base(path), Name: name, Port: 22}
	data, err := os.ReadFile(path)
	if err != nil {
		p.SkipReason = fmt.Sprintf("read: %v", err)
		return p
	}
	doc := ParseINI(decodeText(data))

	conn := section(doc, "CONNECTION")
	auth := section(doc, "CONNECTION:AUTHENTICATION")

	if proto := lookup(conn, "Protocol"); proto != "" && !strings.EqualFold(proto, "SSH") {
		p.SkipReason = fmt.Sprintf("非 SSH 协议 (%s)", proto)
		return p
	}
	host := lookup(conn, "Host")
	if host == "" {
		p.SkipReason = "缺少 Host 字段"
		return p
	}
	p.Host = host
	if port := lookup(conn, "Port"); port != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(port)); err == nil && n > 0 && n <= 65535 {
			p.Port = n
		}
	}
	p.Username = lookup(auth, "UserName")
	switch strings.ToUpper(lookup(auth, "Method")) {
	case "PUBLICKEY":
		p.AuthType = "key"
		p.KeyName = lookup(auth, "UserKey")
	case "KEYBOARD_INTERACTIVE", "KEYBOARD_INTERACTIVE_WITH_FALLBACK":
		p.AuthType = "keyboard_interactive"
	default:
		// PASSWORD 与空值（Xshell 老版本）均按密码认证处理
		p.AuthType = "password"
	}
	if p.Username == "" {
		p.SkipReason = "缺少用户名"
		return p
	}
	p.Valid = true
	return p
}

// section 取分节（大小写不敏感；未找到返回空 map）
func section(doc map[string]map[string]string, name string) map[string]string {
	for k, v := range doc {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return map[string]string{}
}

// lookup 分节内取键（大小写不敏感）
func lookup(section map[string]string, key string) string {
	for k, v := range section {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ParseINI 解析简易 INI：[Section] 行 + Key=Value 行（首个 = 分割），
// 分号/井号开头为注释；同名后值覆盖前值。
// 分节名归一为小写（消费方经 section() 大小写不敏感读取，行为一致）。
func ParseINI(text string) map[string]map[string]string {
	doc := map[string]map[string]string{}
	current := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if _, ok := doc[current]; !ok {
				doc[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		doc[current][key] = val
	}
	return doc
}

// decodeText 按 BOM/内容嗅探解码 INI 文本（Xshell 不同版本分别使用
// UTF-16LE（带/不带 BOM）与 UTF-8 存储）。
func decodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return utf16Decode(data[2:], true)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return utf16Decode(data[2:], false)
	case bytes.IndexByte(data, 0) >= 0 && len(data)%2 == 0:
		// 无 BOM 但含 NUL：按 UTF-16LE 处理（Xshell 7 常见）
		return utf16Decode(data, true)
	default:
		return string(data)
	}
}

func utf16Decode(data []byte, littleEndian bool) string {
	u := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if littleEndian {
			u = append(u, uint16(data[i])|uint16(data[i+1])<<8)
		} else {
			u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
		}
	}
	return string(utf16.Decode(u))
}
