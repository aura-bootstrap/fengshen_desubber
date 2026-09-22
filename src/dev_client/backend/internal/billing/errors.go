package billing

import (
	"errors"
	"fmt"
)

// 类型化错误:供上层(HTTP handler / runner)映射中文文案与 HTTP 状态码。
// 哨兵错误用 errors.Is 判断,余额不足用 errors.As 取 need/have。
var (
	ErrCardInvalid      = errors.New("卡密无效")
	ErrCardRevoked      = errors.New("卡密已被吊销")
	ErrBoundOther       = errors.New("卡密已绑定其它设备")
	ErrMachineMismatch  = errors.New("本机机器码与激活设备不一致")
	ErrCardNotActivated = errors.New("卡密尚未在该服务激活")
	ErrTaskNotReady     = errors.New("云端任务尚未完成,暂不能下载")
)

// InsufficientBalanceError 余额不足(402):Need 本次所需点数,Have 当前余额。
type InsufficientBalanceError struct {
	Need int
	Have int
	Msg  string // 服务端原始文案
}

func (e *InsufficientBalanceError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("余额不足: 需要 %d 点,当前 %d 点", e.Need, e.Have)
}

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
		return fmt.Sprintf("计费服务错误(%d): %s", e.Status, e.Msg)
	}
	return fmt.Sprintf("计费服务错误(%d)", e.Status)
}

// Message 把类型化错误映射为面向用户的中文文案。
func Message(err error) string {
	var insuff *InsufficientBalanceError
	switch {
	case errors.As(err, &insuff):
		return fmt.Sprintf("余额不足: 本次需要 %d 点,当前剩余 %d 点,请充值后重试", insuff.Need, insuff.Have)
	case errors.Is(err, ErrCardInvalid):
		return "卡密无效,请核对后重试"
	case errors.Is(err, ErrCardRevoked):
		return "卡密已被吊销,请联系售卡方"
	case errors.Is(err, ErrBoundOther):
		return "该卡密已绑定其它设备,请先在原设备解绑或联系售卡方"
	case errors.Is(err, ErrMachineMismatch):
		return "本机机器码与激活设备不一致(更换硬件后需重新激活)"
	case errors.Is(err, ErrCardNotActivated):
		return "卡密尚未激活,请先激活"
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
	return "计费服务不可达: " + err.Error()
}
