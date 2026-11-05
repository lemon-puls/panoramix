package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lemon-puls/panoramix/internal/store"
)

// QueryError 是跨 Wails 边界的稳定错误：Error() 固定为 "code: message"。
// Wails 只会把 error 转成字符串，因此前端必须按 Code 映射 i18n，
// 不能按 Message（可能随文案/数据库版本变化）匹配。
type QueryError struct {
	Code    string // 稳定错误码，见设计文档 §4.4
	Message string // 兜底展示；可含表名/列名，禁止拼接单元格值
}

func (e *QueryError) Error() string { return e.Code + ": " + e.Message }

// queryErrf 构造带稳定错误码的错误
func queryErrf(code, format string, args ...any) error {
	return &QueryError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorCode 提取错误码；非 *QueryError 返回空串。
// app 层日志只记录 code，不记录可能含数据的 message。
func ErrorCode(err error) string {
	var qe *QueryError
	if errors.As(err, &qe) {
		return qe.Code
	}
	return ""
}

// 稳定错误码（设计文档 §4.4 / §4.5）
const (
	ErrCodeFilterModeConflict       = "filter_mode_conflict"
	ErrCodeTooManyConditions        = "too_many_conditions"
	ErrCodeUnknownColumn            = "unknown_column"
	ErrCodeOperatorNotAllowed       = "operator_not_allowed"
	ErrCodeUnsafeRawWhere           = "unsafe_raw_where"
	ErrCodeRawWhereTooLong          = "raw_where_too_long"
	ErrCodeValueTooLong             = "value_too_long"
	ErrCodeInvalidPage              = "invalid_page"
	ErrCodeInvalidSort              = "invalid_sort"
	ErrCodeTableMetadataUnavailable = "table_metadata_unavailable"
	ErrCodeTooManyEdits             = "too_many_edits"
	ErrCodeApplyEditsFailed         = "apply_edits_failed"
	ErrCodePendingGuardRequired     = "pending_guard_required"
)

const (
	maxFilterConditions = 5
	maxConditionValue   = 4096 // runes
	maxRawWhere         = 4000 // runes
	maxPageOffset       = 1_000_000_000
)

// FilterOp 是结构化筛选操作符白名单
type FilterOp string

const (
	OpEq        FilterOp = "eq"
	OpNe        FilterOp = "ne"
	OpGt        FilterOp = "gt"
	OpGte       FilterOp = "gte"
	OpLt        FilterOp = "lt"
	OpLte       FilterOp = "lte"
	OpContains  FilterOp = "contains"
	OpIsNull    FilterOp = "is_null"
	OpIsNotNull FilterOp = "is_not_null"
)

// FilterCondition 一条结构化条件（同列可重复，用于范围）
type FilterCondition struct {
	Column string   `json:"column"`
	Op     FilterOp `json:"op"`
	Value  string   `json:"value"`
}

// TableQuery 数据标签的一次查询（结构化条件与 RawWhere 互斥）
type TableQuery struct {
	Database   string            `json:"database"`
	Table      string            `json:"table"`
	Conditions []FilterCondition `json:"conditions"`
	RawWhere   string            `json:"rawWhere"`
	SortColumn string            `json:"sortColumn"`
	SortDesc   bool              `json:"sortDesc"`
	Page       int               `json:"page"`
	PageSize   int               `json:"pageSize"`
}

// TableQueryPreview 可读 SQL 预览（值已内联）
type TableQueryPreview struct {
	SQL        string `json:"sql"`
	Where      string `json:"where"` // 不含 WHERE 关键字，用于模式切换预填
	ParamCount int    `json:"paramCount"`
}

// TableDataResponse 数据标签查询结果（列元数据与结果同一次返回）
type TableDataResponse struct {
	Result      *Result      `json:"result"`
	Columns     []ColumnInfo `json:"columns"`
	ExecutedSQL string       `json:"executedSql"`
}

// builtQuery 同时产出执行 SQL、参数与可读 SQL
type builtQuery struct {
	SQL     string
	Args    []any
	Preview string
	Where   string
}

// columnFamily 列类型族（只看 SHOW FULL COLUMNS 的 Type 前缀）
type columnFamily int

const (
	famNumeric columnFamily = iota
	famDate
	famText
	famEnum
	famJSON
	famBinary
	famOther
)

func columnFamilyOf(typeName string) columnFamily {
	t := strings.ToLower(strings.TrimSpace(typeName))
	hasAnyPrefix := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(t, p) {
				return true
			}
		}
		return false
	}
	switch {
	case hasAnyPrefix("tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"decimal", "numeric", "float", "double", "real", "bit"):
		return famNumeric
	case hasAnyPrefix("date", "datetime", "timestamp", "time", "year"):
		return famDate
	case hasAnyPrefix("char", "varchar", "tinytext", "text", "mediumtext", "longtext"):
		return famText
	case hasAnyPrefix("enum", "set"):
		return famEnum
	case hasAnyPrefix("json"):
		return famJSON
	case hasAnyPrefix("binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob"):
		return famBinary
	default:
		return famOther
	}
}

// opAllowed 操作符 × 类型矩阵（设计文档 §3.3.3）
func opAllowed(fam columnFamily, op FilterOp) bool {
	switch op {
	case OpIsNull, OpIsNotNull:
		return true
	case OpEq, OpNe:
		return fam == famNumeric || fam == famDate || fam == famText || fam == famEnum
	case OpGt, OpGte, OpLt, OpLte:
		return fam == famNumeric || fam == famDate
	case OpContains:
		return fam == famText || fam == famJSON
	default:
		return false
	}
}

// escapeLike 转义 LIKE 通配符；使用 ! 作为 ESCAPE（避免 NO_BACKSLASH_ESCAPES）
func escapeLike(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if r == '!' || r == '%' || r == '_' {
			b.WriteByte('!')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sqlLiteralString 把值转成可读 SQL 字符串字面量（仅用于预览/复制）。
// 执行路径始终走参数化，不受 NO_BACKSLASH_ESCAPES 影响。
func sqlLiteralString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString("''")
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0:
			b.WriteString(`\0`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// findColumn 按列名查找（列名来自同一份元数据，精确匹配）
func findColumn(cols []ColumnInfo, name string) (ColumnInfo, bool) {
	for _, c := range cols {
		if c.Field == name {
			return c, true
		}
	}
	return ColumnInfo{}, false
}

// analyzeRawWhere 词法级校验高级 WHERE，并去掉恰好一个前导 WHERE。
//
// 这不是安全沙箱（用户拥有数据库凭据）；拦截顶层分号/注释/子句是为了
// 防止误操作破坏系统追加的 ORDER BY / LIMIT。字符串/反引号/括号内内容跳过。
func analyzeRawWhere(raw string) (string, bool, error) {
	// 控制字符：保留换行/Tab/回车，其余拒绝
	for _, r := range raw {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return "", false, queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE contains control characters")
		}
	}

	trimmed := strings.TrimSpace(raw)
	normalized, hadWhere := stripLeadingWhere(trimmed)
	if err := scanRawWhere(normalized); err != nil {
		return "", false, err
	}
	return normalized, hadWhere, nil
}

// stripLeadingWhere 只去掉最前面一个裸 WHERE（带反引号的标识符/字符串不算）
func stripLeadingWhere(s string) (string, bool) {
	if len(s) < 5 || !strings.EqualFold(s[:5], "WHERE") {
		return s, false
	}
	rest := s[5:]
	if rest == "" {
		return "", true
	}
	r, size := utf8.DecodeRuneInString(rest)
	if !unicode.IsSpace(r) {
		return s, false
	}
	return strings.TrimSpace(rest[size:]), true
}

// scanRawWhere 扫描顶层 token，拒绝分号/注释/系统子句
func scanRawWhere(s string) error {
	tokens := make([]string, 0, 16)
	depth := 0
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case c == '\'' || c == '"':
			next, ok := skipQuoted(s, i, c)
			if !ok {
				return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE has an unterminated string")
			}
			i = next
		case c == '`':
			next, ok := skipBacktick(s, i)
			if !ok {
				return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE has an unterminated identifier")
			}
			i = next
		case c == '-' && i+1 < n && s[i+1] == '-':
			return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain comments")
		case c == '#':
			return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain comments")
		case c == '/' && i+1 < n && s[i+1] == '*':
			// 包括 MySQL 版本注释 /*!50000 ... */（内部 SQL 会执行）
			return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain comments")
		case c == ';' && depth == 0:
			return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain a top-level semicolon")
		case c == '(':
			depth++
			i++
		case c == ')':
			if depth > 0 {
				depth--
			}
			i++
		case isWordByte(c):
			start := i
			for i < n && isWordByte(s[i]) {
				i++
			}
			if depth == 0 {
				tokens = append(tokens, strings.ToUpper(s[start:i]))
			}
		default:
			i++
		}
	}

	for i, tok := range tokens {
		switch tok {
		case "ORDER", "GROUP":
			if i+1 < len(tokens) && tokens[i+1] == "BY" {
				return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain %s BY", tok)
			}
		case "HAVING", "LIMIT", "OFFSET", "UNION":
			return queryErrf(ErrCodeUnsafeRawWhere, "raw WHERE cannot contain %s", tok)
		}
	}
	return nil
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c >= 0x80
}

// skipQuoted 跳过字符串字面量（支持反斜杠转义与双写引号）
func skipQuoted(s string, start int, quote byte) (int, bool) {
	i := start + 1
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1, true
		}
		i++
	}
	return 0, false
}

// skipBacktick 跳过反引号标识符（双写反引号转义）
func skipBacktick(s string, start int) (int, bool) {
	i := start + 1
	for i < len(s) {
		if s[i] == '`' {
			if i+1 < len(s) && s[i+1] == '`' {
				i += 2
				continue
			}
			return i + 1, true
		}
		i++
	}
	return 0, false
}

// validateAndBuild 是核心纯函数：注入假列定义即可单测，不依赖真实 MySQL。
func validateAndBuild(cols []ColumnInfo, q TableQuery) (*builtQuery, error) {
	if strings.TrimSpace(q.Database) == "" || strings.TrimSpace(q.Table) == "" {
		return nil, queryErrf(ErrCodeTableMetadataUnavailable, "database and table are required")
	}
	rawWhere := strings.TrimSpace(q.RawWhere)
	if len(q.Conditions) > 0 && rawWhere != "" {
		return nil, queryErrf(ErrCodeFilterModeConflict, "conditions and raw WHERE are mutually exclusive")
	}
	if len(q.Conditions) > maxFilterConditions {
		return nil, queryErrf(ErrCodeTooManyConditions, "%d conditions (max %d)", len(q.Conditions), maxFilterConditions)
	}

	var whereParts, previewParts []string
	var args []any

	for _, cond := range q.Conditions {
		col, ok := findColumn(cols, cond.Column)
		if !ok {
			return nil, queryErrf(ErrCodeUnknownColumn, "column %q does not exist", cond.Column)
		}
		if !opAllowed(columnFamilyOf(col.Type), cond.Op) {
			return nil, queryErrf(ErrCodeOperatorNotAllowed,
				"operator %q is not allowed for column %q (%s)", cond.Op, cond.Column, col.Type)
		}
		if utf8.RuneCountInString(cond.Value) > maxConditionValue {
			return nil, queryErrf(ErrCodeValueTooLong, "condition value exceeds %d characters", maxConditionValue)
		}

		ident := quoteIdent(cond.Column)
		switch cond.Op {
		case OpEq, OpNe, OpGt, OpGte, OpLt, OpLte:
			sqlOp := map[FilterOp]string{
				OpEq: "=", OpNe: "<>", OpGt: ">", OpGte: ">=", OpLt: "<", OpLte: "<=",
			}[cond.Op]
			whereParts = append(whereParts, ident+" "+sqlOp+" ?")
			previewParts = append(previewParts, ident+" "+sqlOp+" "+sqlLiteralString(cond.Value))
			args = append(args, cond.Value)
		case OpContains:
			pattern := "%" + escapeLike(cond.Value) + "%"
			whereParts = append(whereParts, ident+" LIKE ? ESCAPE '!'")
			previewParts = append(previewParts, ident+" LIKE "+sqlLiteralString(pattern)+" ESCAPE '!'")
			args = append(args, pattern)
		case OpIsNull:
			whereParts = append(whereParts, ident+" IS NULL")
			previewParts = append(previewParts, ident+" IS NULL")
		case OpIsNotNull:
			whereParts = append(whereParts, ident+" IS NOT NULL")
			previewParts = append(previewParts, ident+" IS NOT NULL")
		default:
			return nil, queryErrf(ErrCodeOperatorNotAllowed, "unknown operator %q", cond.Op)
		}
	}

	whereSQL := strings.Join(whereParts, " AND ")
	wherePreview := strings.Join(previewParts, " AND ")
	if rawWhere != "" {
		if utf8.RuneCountInString(rawWhere) > maxRawWhere {
			return nil, queryErrf(ErrCodeRawWhereTooLong, "raw WHERE exceeds %d characters", maxRawWhere)
		}
		normalized, _, err := analyzeRawWhere(rawWhere)
		if err != nil {
			return nil, err
		}
		whereSQL = normalized
		wherePreview = normalized
	}

	// 排序列必须存在
	if q.SortColumn != "" {
		if _, ok := findColumn(cols, q.SortColumn); !ok {
			return nil, queryErrf(ErrCodeUnknownColumn, "sort column %q does not exist", q.SortColumn)
		}
	}

	// 分页：先算 offset 再校验上限，避免 page 上限与 offset 上限不一致
	if q.Page < 0 {
		return nil, queryErrf(ErrCodeInvalidPage, "page must be >= 0")
	}
	switch q.PageSize {
	case 100, 500, 1000, 5000:
	default:
		return nil, queryErrf(ErrCodeInvalidPage, "page size %d is not supported", q.PageSize)
	}
	offset := int64(q.Page) * int64(q.PageSize)
	if offset > maxPageOffset {
		return nil, queryErrf(ErrCodeInvalidPage, "page offset %d exceeds limit", offset)
	}

	from := "SELECT * FROM " + quoteIdent(q.Database) + "." + quoteIdent(q.Table)
	sortSQL := ""
	if q.SortColumn != "" {
		dir := "ASC"
		if q.SortDesc {
			dir = "DESC"
		}
		sortSQL = " ORDER BY " + quoteIdent(q.SortColumn) + " " + dir
	}
	suffixSQL := ""
	if whereSQL != "" {
		suffixSQL = " WHERE " + whereSQL
	}
	sqlText := from + suffixSQL + sortSQL + " LIMIT ? OFFSET ?"
	args = append(args, q.PageSize, offset)

	suffixPreview := suffixSQL
	if wherePreview != "" {
		suffixPreview = " WHERE " + wherePreview
	}
	preview := from + suffixPreview + sortSQL +
		" LIMIT " + strconv.Itoa(q.PageSize) + " OFFSET " + strconv.FormatInt(offset, 10)

	return &builtQuery{
		SQL:     sqlText,
		Args:    args,
		Preview: preview,
		Where:   wherePreview,
	}, nil
}

// TableData 执行一次数据标签查询（结构化条件参数化，值与 LIMIT/OFFSET 全部走占位符）
func (m *Manager) TableData(c store.DBConn, password string, q TableQuery) (*TableDataResponse, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cols, err := queryFullColumns(ctx, db, q.Database, q.Table)
	if err != nil {
		return nil, err
	}
	built, err := validateAndBuild(cols, q)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res, err := queryArgs(ctx, db, built.SQL, built.Args, q.PageSize)
	if err != nil {
		return nil, err
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return &TableDataResponse{Result: res, Columns: cols, ExecutedSQL: built.Preview}, nil
}

// PreviewTableSQL 返回与执行路径同一 builder 生成的可读 SQL（不执行）
func (m *Manager) PreviewTableSQL(c store.DBConn, password string, q TableQuery) (*TableQueryPreview, error) {
	db, err := m.Open(c, password)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cols, err := queryFullColumns(ctx, db, q.Database, q.Table)
	if err != nil {
		return nil, err
	}
	built, err := validateAndBuild(cols, q)
	if err != nil {
		return nil, err
	}
	return &TableQueryPreview{
		SQL:        built.Preview,
		Where:      built.Where,
		ParamCount: len(built.Args),
	}, nil
}
