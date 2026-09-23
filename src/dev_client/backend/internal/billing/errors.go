package billing

import (
	"errors"
	"fmt"
)

// 类型化错误:供上层(HTTP handler / runner)映射中文文案与 HTTP 状态码。
var (
	ErrAuthInvalid  = errors.New("云端账号或密码无效")
	ErrTaskNotReady = errors.New("云端任务尚未完成,暂不能下载")
)

type ObjectStoreError struct {
	Status    int
	Code      string
	Msg       string
	RequestID string
	Cause     error
}

func (e *ObjectStoreError) Error() string {
	if e.Cause != nil {
		return "对象存储上传失败: " + e.Cause.Error()
	}
	detail := e.Code
	if e.Msg != "" {
		if detail != "" {
			detail += ": "
		}
		detail += e.Msg
	}
	if e.RequestID != "" {
		if detail != "" {
			detail += ", "
		}
		detail += "请求ID " + e.RequestID
	}
	if detail != "" {
		return fmt.Sprintf("对象存储上传失败(%d): %s", e.Status, detail)
	}
	return fmt.Sprintf("对象存储上传失败(%d)", e.Status)
}

// APIError 未归类的服务端错误(带 HTTP 状态与服务端错误码/文案)。
type APIError struct {
	Status int
	Code   string
	Msg    string
}

func (e *APIError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("云端服务错误(%d): %s", e.Status, e.Msg)
	}
	return fmt.Sprintf("云端服务错误(%d)", e.Status)
}

// Message 把类型化错误映射为面向使用者的中文文案。
func Message(err error) string {
	switch {
	case errors.Is(err, ErrAuthInvalid):
		return "云端账号认证失败,请检查地址、账号或密码"
	case errors.Is(err, ErrTaskNotReady):
		return "云端任务尚未完成,暂不能下载"
	}
	var storeErr *ObjectStoreError
	if errors.As(err, &storeErr) {
		return storeErr.Error()
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Error()
	}
	return "云端服务不可达: " + err.Error()
}
