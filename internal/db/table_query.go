package db

import (
	"errors"
	"fmt"
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
