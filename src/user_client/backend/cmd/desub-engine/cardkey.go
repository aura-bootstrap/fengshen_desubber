package main

// 卡密激活体系:status/activate/deactivate 三个端点。
// cardkey.json 存 exe 旁(DPAPI 整体加密),卡面只在激活请求与内存中出现,
// 对前端只回脱敏形态(留首组+末字符)。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"fengshen-desub/internal/billing"
	"fengshen-desub/internal/cardkey"
)

// handleMachineAccountStatus GET /api/account/status。
// 本地只保存访问凭据；余额仅在远端机器账户实时查询成功时返回。
func (s *server) handleMachineAccountStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"linked":            false,
		"balance_available": false,
		"machine_hash":      "",
		"degraded":          false,
	}
	kf, err := cardkey.LoadKeyFile(s.exeDir)
	if errors.Is(err, cardkey.ErrNotActivated) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		// 授权数据损坏:按未激活答,但给出原因引导用户重新激活(不擅自删文件)。
		resp["error"] = "本地授权数据无法读取,请重新激活"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// 机器码实时采集(失败则回退到激活时存的哈希)。
	hash, degraded, err := cardkey.MachineID()
	if err != nil {
		hash, degraded = kf.MachineHash, false
	}
	resp["linked"] = true
	resp["machine_hash"] = hashMask(hash)
	resp["degraded"] = degraded

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	account, err := billing.New(kf.Server, kf.CardKey, kf.MachineHash).Account(ctx)
	if err != nil {
		resp["error"] = billing.Message(err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["balance"] = account.Balance
	resp["balance_available"] = true
	writeJSON(w, http.StatusOK, resp)
}

// handleRedeemCard POST /api/cards/redeem {"card_key"}。
// 计费服务地址用二进制内置的 billing.ServerURL，前端只传充值卡卡面。
func (s *server) handleRedeemCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CardKey string `json:"card_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求解析失败")
		return
	}
	server := strings.TrimSpace(billing.ServerURL)
	cardKey := strings.ToUpper(strings.TrimSpace(req.CardKey))
	if cardKey == "" {
		writeErr(w, http.StatusUnprocessableEntity, "卡密不能为空")
		return
	}
	if server == "" {
		writeErr(w, http.StatusInternalServerError, "授权服务端地址未配置(请升级或联系发行方)")
		return
	}
	hash, degraded, err := cardkey.MachineID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "机器码采集失败: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	redeemed, err := billing.New(server, cardKey, hash).RedeemCard(ctx)
	if err != nil {
		writeErr(w, redeemHTTPCode(err), billing.Message(err))
		return
	}
	kf := &cardkey.KeyFile{
		Server:      server,
		CardKey:     cardKey,
		MachineHash: hash,
		ActivatedAt: time.Now().Unix(),
	}
	if err := cardkey.SaveKeyFile(s.exeDir, kf); err != nil {
		writeErr(w, http.StatusInternalServerError, "卡密落盘失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"linked":            true,
		"machine_hash":      hashMask(redeemed.Account.MachineHash),
		"degraded":          degraded,
		"balance_available": true,
		"balance":           redeemed.Account.Balance,
	})
}

// handleClearAccountCredential DELETE /api/account/credential。
// 只删本地访问凭据，不解除远端充值卡与机器账户的绑定。
func (s *server) handleClearAccountCredential(w http.ResponseWriter, r *http.Request) {
	if err := cardkey.ClearKeyFile(s.exeDir); err != nil {
		writeErr(w, http.StatusInternalServerError, "解除激活失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// redeemHTTPCode 核销失败的 HTTP 码映射，网络类统一 502。
func redeemHTTPCode(err error) int {
	switch {
	case errors.Is(err, billing.ErrCardInvalid):
		return http.StatusUnauthorized
	case errors.Is(err, billing.ErrCardRevoked), errors.Is(err, billing.ErrCardNotActivated):
		return http.StatusForbidden
	case errors.Is(err, billing.ErrBoundOther):
		return http.StatusConflict
	}
	var apiErr *billing.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 600 {
		return apiErr.Status
	}
	return http.StatusBadGateway
}

// hashMask 机器码展示值:取前 16 位按 XXXX-XXXX-XXXX-XXXX 分组明文,不打码,
// 与 slicer 管理端 groupMachine16 同则、两端显示同一串便于报障核对。
// 仅作用于展示出口;协议与存储仍为全长 64 位。不足 16 位按实际长度分组。
func hashMask(h string) string {
	s := strings.ToUpper(strings.TrimSpace(h))
	if len(s) > 16 {
		s = s[:16]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
