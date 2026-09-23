package main

// 开发版云端内部通道凭据:status/login/clear 三个本地端点。
// 凭据为云端服务管理员账号,落 cloudauth.json(DPAPI 整体加密);不保存卡面或余额。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aura-bootstrap/fengshen_desubber/internal/billing"
	"github.com/aura-bootstrap/fengshen_desubber/internal/cardkey"
)

// handleCloudStatus GET /api/cloud/status。
// 已配置凭据时顺带做一次远端登录验证;不可达时不把历史状态当成可用。
func (s *server) handleCloudStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"linked":       false,
		"online":       false,
		"server":       "",
		"username":     "",
		"machine_hash": "",
		"degraded":     false,
	}
	cred, err := cardkey.LoadCredential(s.exeDir)
	if errors.Is(err, cardkey.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		resp["error"] = "本地云端凭据无法读取,请重新配置"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["linked"] = true
	resp["server"] = cred.Server
	resp["username"] = cred.Username
	if hash, degraded, err := cardkey.MachineID(); err == nil {
		resp["machine_hash"] = hashMask(hash)
		resp["degraded"] = degraded
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := billing.Login(ctx, cred.Server, cred.Username, cred.Password); err != nil {
		resp["error"] = billing.Message(err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["online"] = true
	writeJSON(w, http.StatusOK, resp)
}

// handleCloudLogin POST /api/cloud/login {"server"?, "username", "password"}。
// server 缺省取全局配置 online.server;登录成功才把凭据落盘。
func (s *server) handleCloudLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
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
	username := strings.TrimSpace(req.Username)
	password := req.Password
	if server == "" || username == "" || password == "" {
		writeErr(w, http.StatusUnprocessableEntity, "云端服务地址、账号和密码不能为空")
		return
	}
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		writeErr(w, http.StatusUnprocessableEntity, "云端服务地址须为 http(s):// 地址")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := billing.Login(ctx, server, username, password); err != nil {
		writeErr(w, http.StatusUnauthorized, billing.Message(err))
		return
	}
	cred := &cardkey.CloudCredential{
		Server: server, Username: username, Password: password, SavedAt: time.Now().Unix(),
	}
	if err := cardkey.SaveCredential(s.exeDir, cred); err != nil {
		writeErr(w, http.StatusInternalServerError, "云端凭据落盘失败: "+err.Error())
		return
	}
	hash, degraded, machineErr := cardkey.MachineID()
	if machineErr != nil {
		hash, degraded = "", false
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"linked":       true,
		"online":       true,
		"server":       server,
		"username":     username,
		"machine_hash": hashMask(hash),
		"degraded":     degraded,
	})
}

// handleClearCloudCredential DELETE /api/cloud/credential。
// 只删本机凭据,不调远端 logout(避免吊销管理员账号的全部会话)。
func (s *server) handleClearCloudCredential(w http.ResponseWriter, r *http.Request) {
	if err := cardkey.ClearCredential(s.exeDir); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除云端凭据失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// hashMask 机器码展示值:取前 16 位按 XXXX-XXXX-XXXX-XXXX 分组明文,
// 与管理端 groupMachine16 同则、两端显示同一串便于报障核对。
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
