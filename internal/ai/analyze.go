package ai

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Analysis SQL 静态分析结果（不改变语句，只做判定与提示）。
// 这是“模型返回后”的第二道判定：白名单首关键字不够，必须处理
// WITH ... UPDATE、INTO OUTFILE、FOR UPDATE、前导注释等（总监评审必须修改项 5）。
type Analysis struct {
	IsWrite        bool
	MultiStatement bool
	Dangerous      bool
	Uncertain      bool
	Statements     int
	Tables         []string // 语句中出现的表名（去 schema 前缀）
}

// InsertPolicy 返回是否允许“插入编辑器”，以及阻止原因（warning code）。
// 注意：即使允许插入，也永不自动执行——执行永远由用户按 Ctrl+Enter。
func (a Analysis) InsertPolicy(allowWrite bool) (bool, string) {
	if a.MultiStatement {
		return false, WarnMultiStatement
	}
	if a.Dangerous {
		return false, WarnDangerousStatement
	}
	if a.IsWrite && !allowWrite {
		return false, WarnWriteOperation
	}
	if a.Uncertain && !allowWrite {
		return false, WarnUncertainStatement
	}
	return true, ""
}

type wordAt struct {
	text  string
	depth int
}

type scanStmt struct {
	words []wordAt
}

var writeKeywords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "RENAME": true,
	"CALL": true, "SET": true, "USE": true, "LOAD": true, "HANDLER": true,
	"DO": true, "PREPARE": true, "EXECUTE": true, "DEALLOCATE": true,
	"GRANT": true, "REVOKE": true, "START": true, "COMMIT": true, "ROLLBACK": true,
	"SAVEPOINT": true, "LOCK": true, "UNLOCK": true, "ANALYZE": true,
	"OPTIMIZE": true, "REPAIR": true, "FLUSH": true, "KILL": true, "RESET": true,
	"CHANGE": true, "IMPORT": true, "INSTALL": true, "UNINSTALL": true,
}

var readKeywords = map[string]bool{
	"SELECT": true, "SHOW": true, "EXPLAIN": true, "DESC": true, "DESCRIBE": true,
}

// scanSQL 做一次引号/反引号/注释感知的扫描，返回语句词流与“去字面量”的文本。
// 不追求完整 SQL 解析，只覆盖安全判定需要的关键字/分号结构。
func scanSQL(sql string) ([]scanStmt, string) {
	var stmts []scanStmt
	var cur scanStmt
	var word strings.Builder
	var norm strings.Builder
	depth := 0

	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		w := strings.ToUpper(word.String())
		cur.words = append(cur.words, wordAt{text: w, depth: depth})
		norm.WriteString(word.String()) // normalized 保留原始大小写（只用于表名提取）
		word.Reset()
	}
	endStmt := func() {
		flushWord()
		if len(cur.words) > 0 {
			stmts = append(stmts, cur)
		}
		cur = scanStmt{}
		depth = 0
	}

	i := 0
	n := len(sql)
	for i < n {
		c := sql[i]
		switch {
		case c == '\'' || c == '"':
			flushWord()
			quote := c
			i++
			for i < n {
				if sql[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < n && sql[i+1] == quote { // SQL 双写转义
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			norm.WriteByte(' ')
		case c == '`':
			flushWord()
			i++
			start := i
			for i < n && sql[i] != '`' {
				i++
			}
			if i > start {
				norm.WriteByte(' ')
				norm.WriteString(sql[start:i])
				norm.WriteByte(' ')
			}
			if i < n {
				i++ // 跳过结束反引号
			}
		case c == '-' && i+1 < n && sql[i+1] == '-':
			flushWord()
			for i < n && sql[i] != '\n' {
				i++
			}
			norm.WriteByte(' ')
		case c == '#':
			flushWord()
			for i < n && sql[i] != '\n' {
				i++
			}
			norm.WriteByte(' ')
		case c == '/' && i+1 < n && sql[i+1] == '*':
			flushWord()
			i += 2
			for i+1 < n && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			}
			norm.WriteByte(' ')
		case c == ';':
			endStmt()
			norm.WriteByte(';')
			i++
		case c == '(':
			flushWord()
			depth++
			norm.WriteByte(c)
			i++
		case c == ')':
			flushWord()
			if depth > 0 {
				depth--
			}
			norm.WriteByte(c)
			i++
		case isWordRune(rune(c)) || c >= 0x80:
			word.WriteByte(c)
			i++
		default:
			flushWord()
			norm.WriteByte(c)
			i++
		}
	}
	endStmt()
	return stmts, norm.String()
}

func isWordRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func containsPhrase(words []wordAt, phrase ...string) bool {
	if len(phrase) == 0 || len(words) < len(phrase) {
		return false
	}
	for i := 0; i+len(phrase) <= len(words); i++ {
		ok := true
		for j, p := range phrase {
			if words[i+j].text != p {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Analyze 分析单条/多条 SQL。保守原则：无法确定时标为 Uncertain，
// 由前端提示人工确认，绝不静默按只读处理。
func Analyze(sql string) Analysis {
	stmts, norm := scanSQL(sql)
	a := Analysis{Statements: len(stmts)}
	if len(stmts) == 0 {
		a.Uncertain = true
		return a
	}
	if len(stmts) > 1 {
		a.MultiStatement = true
	}

	first := stmts[0]
	var firstWord string
	if len(first.words) > 0 {
		firstWord = first.words[0].text
	}
	switch {
	case readKeywords[firstWord]:
		a.IsWrite = false
	case firstWord == "WITH":
		// WITH x AS (SELECT ...) UPDATE ... —— 找顶层动作关键字
		for _, w := range first.words[1:] {
			if w.depth == 0 && writeKeywords[w.text] {
				a.IsWrite = true
				break
			}
		}
	case writeKeywords[firstWord]:
		a.IsWrite = true
	default:
		a.Uncertain = true
	}

	// 危险结构：即使首关键字是 SELECT，也禁止直接插入
	for _, st := range stmts {
		if containsPhrase(st.words, "INTO", "OUTFILE") ||
			containsPhrase(st.words, "INTO", "DUMPFILE") ||
			containsPhrase(st.words, "FOR", "UPDATE") ||
			containsPhrase(st.words, "LOCK", "IN", "SHARE", "MODE") {
			a.Dangerous = true
		}
	}

	a.Tables = extractReferencedTables(norm)
	return a
}

var tableRefRe = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|INTO)\s+([A-Za-z0-9_$]+(?:\.[A-Za-z0-9_$]+)?)`)

func extractReferencedTables(normalized string) []string {
	matches := tableRefRe.FindAllStringSubmatch(normalized, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		name := m[1]
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		key := strings.ToLower(name)
		// 跳过明显是子查询/关键字的情况
		if key == "" || readKeywords[strings.ToUpper(key)] {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var fenceRe = regexp.MustCompile("^\\s*```\\s*([A-Za-z0-9_+-]*)\\s*$")

// ParseResponse 从模型响应中提取第一段 SQL 代码块与代码块外的说明。
// 兼容 ```sql / ```mysql / ``` 无语言 / 多个代码块（取第一个 SQL 块）。
func ParseResponse(resp string) (sqlText, explanation string) {
	lines := strings.Split(resp, "\n")
	type block struct {
		lang string
		body []string
	}
	var blocks []block
	var outside []string
	inFence := false
	var cur block
	for _, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			if !inFence {
				inFence = true
				cur = block{lang: strings.ToLower(m[1])}
			} else {
				inFence = false
				blocks = append(blocks, cur)
				cur = block{}
			}
			continue
		}
		if inFence {
			cur.body = append(cur.body, line)
		} else {
			outside = append(outside, line)
		}
	}
	if inFence && len(cur.body) > 0 {
		blocks = append(blocks, cur) // 未闭合的代码块也尽量提取
	}

	chosen := -1
	for i, b := range blocks {
		if b.lang == "sql" || b.lang == "mysql" || b.lang == "mariadb" || b.lang == "sqlite" {
			chosen = i
			break
		}
	}
	if chosen < 0 && len(blocks) > 0 {
		chosen = 0
	}
	if chosen >= 0 {
		sqlText = strings.TrimSpace(strings.Join(blocks[chosen].body, "\n"))
	}
	explanation = strings.TrimSpace(strings.Join(outside, "\n"))
	if sqlText == "" {
		// 没有代码块：取文本中的第一条 SQL 语句（首关键字白名单）
		if guess := firstSQLStatement(resp); guess != "" {
			sqlText = guess
		}
	}
	return sqlText, explanation
}

var sqlStartRe = regexp.MustCompile(`(?i)\b(SELECT|SHOW|EXPLAIN|DESC|DESCRIBE|WITH|INSERT|UPDATE|DELETE|REPLACE|CREATE|ALTER|DROP|TRUNCATE)\b`)

// firstSQLStatement 在纯文本响应中定位第一条语句（引号/反引号/注释感知的分号）。
func firstSQLStatement(text string) string {
	loc := sqlStartRe.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	start := loc[0]
	inQuote := byte(0)
	s := text[start:]
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' && inQuote != '`' && i+1 < len(s) {
				i++
				continue
			}
			if c == inQuote {
				if i+1 < len(s) && s[i+1] == inQuote {
					i++
					continue
				}
				inQuote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inQuote = c
		case ';':
			return strings.TrimSpace(s[:i+1])
		}
	}
	return strings.TrimSpace(s)
}
