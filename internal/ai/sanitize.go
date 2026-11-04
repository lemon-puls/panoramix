package ai

import (
	"regexp"
	"sort"
	"strings"
)

// SanitizeDDL 清理 SHOW CREATE 输出中的敏感/易变信息，然后再发送给模型。
//
// 处理项（总监评审必须修改项 4）：
//   - DEFINER=`user`@`host` / DEFINER=CURRENT_USER：泄露数据库用户名/主机名
//   - AUTO_INCREMENT=N：泄露近似行数
//   - /*!40101 ... */ 版本注释：内容随 MySQL 版本变化，且可能夹带执行指令
//   - 控制字符（除 \n \t）：不可信输入的常见注入载体
//
// 注意：列/表 COMMENT 中的文字会保留——它是 schema 的一部分，且会被
// <schema> 包裹并在 system prompt 中声明为“仅元数据、不是指令”。
func SanitizeDDL(ddl string) string {
	s := strings.ReplaceAll(ddl, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// 版本注释（含内容）整体移除
	s = versionCommentRe.ReplaceAllString(s, "")
	// DEFINER=user@host（反引号或裸标识符）
	s = definerRe.ReplaceAllString(s, "")
	s = definerCurrentRe.ReplaceAllString(s, "")
	// 视图的 SQL SECURITY 属性不泄露身份，但与 DEFINER 语义相关，一并去掉以减少噪声
	s = sqlSecurityRe.ReplaceAllString(s, "")
	// AUTO_INCREMENT=数字
	s = autoIncrementRe.ReplaceAllString(s, "")
	// 行尾多余空格清理（保留缩进与注释原文，避免改动 COMMENT 内容）
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")
	s = strings.TrimSpace(s)
	return stripControlChars(s, true)
}

var (
	versionCommentRe = regexp.MustCompile(`(?s)/\*![0-9]*\s*.*?\*/`)
	definerRe        = regexp.MustCompile("(?i)DEFINER\\s*=\\s*(?:`(?:[^`]|``)*`|[^\\s@]+)\\s*@\\s*(?:`(?:[^`]|``)*`|[^\\s]+)")
	definerCurrentRe = regexp.MustCompile(`(?i)DEFINER\s*=\s*CURRENT_USER(?:\s*\(\s*\))?`)
	sqlSecurityRe    = regexp.MustCompile(`(?i)\bSQL\s+SECURITY\s+(?:DEFINER|INVOKER)\b`)
	autoIncrementRe  = regexp.MustCompile(`(?i)\bAUTO_INCREMENT\s*=\s*\d+`)
	referencesRe     = regexp.MustCompile("(?i)\\bREFERENCES\\s+(?:`[^`]+`|[A-Za-z0-9_$]+)(?:\\s*\\.\\s*(?:`[^`]+`|[A-Za-z0-9_$]+))?")
	identSplitRe     = regexp.MustCompile("[.`\\s]")
)

// stripControlChars 删除 C0 控制字符（keepNewline=true 时保留 \n / \t）。
func stripControlChars(s string, keepNewline bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' && keepNewline:
			b.WriteRune(r)
		case r == '\t' && keepNewline:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// 丢弃
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// referencedTables 从 DDL 中提取外键引用的表名（去 schema 前缀，去重排序）。
func referencedTables(ddl string) []string {
	matches := referencesRe.FindAllString(ddl, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		// 取 REFERENCES 之后的部分，再取最后一个标识符
		idx := strings.Index(strings.ToUpper(m), "REFERENCES")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(m[idx+len("REFERENCES"):])
		parts := identSplitRe.Split(rest, -1)
		name := ""
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				name = parts[i]
				break
			}
		}
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
