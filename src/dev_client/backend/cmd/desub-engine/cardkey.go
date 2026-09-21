package main

// 卡密激活体系:status/activate/deactivate 三个端点,移植自用户版 cardkey.go。
// 与用户版差异:无 ldflags 注入地址,激活服务器取请求体 server,缺省回落全局
// 配置 online.server;cardkey.json 同样存 exe 旁(DPAPI 整体加密),对前端只回脱敏形态。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aura-bootstrap/fengshen_desubber/internal/billing"
	"github.com/aura-bootstrap/fengshen_desubber/internal/cardkey"
)

// handleCardkeyStatus GET /api/cardkey/status
// 未激活: {"activated":false};已激活: 顺带远端 balance 刷新余额,
// 远端不可达时返回本地缓存值并带 "stale":true。
func (s *server) handleCardkeyStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"activated":    false,
		"server":       "",
		"masked":       "",
		"credits":      0,
		"machine_hash": "",
		"degraded":     false,
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
	resp["activated"] = true
	resp["server"] = kf.Server
	resp["masked"] = maskCardKey(kf.CardKey)
	resp["machine_hash"] = hashMask(hash)
	resp["degraded"] = degraded
	resp["credits"] = kf.Credits

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	credits, err := billing.New(kf.Server, kf.CardKey, kf.MachineHash).Balance(ctx)
	if err != nil {
		resp["stale"] = true // 远端不可达:回本地缓存值
		if errors.Is(err, billing.ErrMachineMismatch) || errors.Is(err, billing.ErrCardRevoked) ||
			errors.Is(err, billing.ErrCardNotActivated) || errors.Is(err, billing.ErrCardInvalid) {
			resp["error"] = billing.Message(err) // 授权类错误要明确告知
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["credits"] = credits
	// 刷新本地缓存余额(写失败不影响本次响应)。
	kf.Credits = credits
	_ = cardkey.SaveKeyFile(s.exeDir, kf)
	writeJSON(w, http.StatusOK, resp)
}

// handleCardkeyActivate POST /api/cardkey/activate {"card_key", "server"?}
// server 缺省取全局配置 online.server。流程: MachineID() -> 远端 activate -> 成功落 keyfile。
func (s *server) handleCardkeyActivate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CardKey string `json:"card_key"`
		Server  string `json:"server"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求解析失败")
		return
	}
	server := strings.TrimRight(strings.TrimSpace(req.Server), "/")
	if server == "" {
		if cfg, err := loadConfig(s.cfgPath); err == nil {
			server = strings.TrimRight(strings.TrimSpace(cfg.Online.Server), "/")
		}
	}
	cardKey := strings.ToUpper(strings.TrimSpace(req.CardKey))
	if cardKey == "" {
		writeErr(w, http.StatusUnprocessableEntity, "卡密不能为空")
		return
	}
	if server == "" {
		writeErr(w, http.StatusUnprocessableEntity, "计费服务地址未配置(「在线」配置页填 online.server)")
		return
	}
	hash, _, err := cardkey.MachineID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "机器码采集失败: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	act, err := billing.New(server, cardKey, hash).Activate(ctx)
	if err != nil {
		writeErr(w, activateHTTPCode(err), billing.Message(err))
		return
	}
	kf := &cardkey.KeyFile{
		Server:      server,
		CardKey:     cardKey,
		MachineHash: hash,
		Credits:     act.Credits,
		ActivatedAt: time.Now().Unix(),
	}
	if err := cardkey.SaveKeyFile(s.exeDir, kf); err != nil {
		writeErr(w, http.StatusInternalServerError, "卡密落盘失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activated": true, "credits": act.Credits})
}

// handleCardkeyDeactivate POST /api/cardkey/deactivate
// 只删本地 keyfile,不解远端绑定(同卡在新机器激活会撞 card_bound_other,
// 需售卡方解绑——在响应里说明)。
func (s *server) handleCardkeyDeactivate(w http.ResponseWriter, r *http.Request) {
	if err := cardkey.ClearKeyFile(s.exeDir); err != nil {
		writeErr(w, http.StatusInternalServerError, "解除激活失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// activateHTTPCode 激活失败的 HTTP 码映射:沿用服务端语义码,网络类统一 502。
func activateHTTPCode(err error) int {
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

// maskCardKey 卡面脱敏:留首组(首个 '-' 之前;无分组取前 5 字符)+ 末字符,
// 如 ABCDE-FGHIJ-KLMNO -> ABCDE…O。
func maskCardKey(k string) string {
	k = strings.TrimSpace(k)
	if k == "" {
		return ""
	}
	first := k
	if i := strings.Index(k, "-"); i > 0 {
		first = k[:i]
	} else if utf8.RuneCountInString(k) > 5 {
		first = string([]rune(k)[:5])
	}
	last, _ := utf8.DecodeLastRuneInString(k)
	return first + "…" + string(last)
}
