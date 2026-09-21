package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"fengshen-desubber/billing_server/internal/ddbstore"
)

// 管理员账号与会话端点(仿 slicer core/admin.go):
// login 不限身份(限频+爆破锁),me/logout/password 两角色皆可,
// accounts* 仅 root。会话为 HMAC 无状态令牌,口令纪元 bump 即全吊销。

// login 用户名+密码换会话令牌。限频 20/min/IP;同一 user@IP 连败 10 次锁 30 分钟。
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if n, err := s.st.HitRate(r.Context(), "loginip", ip, 60); err == nil && n > 20 {
		writeErr(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Username == "" || in.Password == "" {
		writeErr(w, http.StatusBadRequest, "username and password required")
		return
	}
	lockID := in.Username + "@" + ip
	if rem, err := s.st.LockedFor(r.Context(), "login", lockID); err == nil && rem > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("locked, retry in %ds", rem))
		return
	}
	a, ok, err := s.st.CheckLogin(r.Context(), in.Username, in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		if _, lerr := s.st.FailLock(r.Context(), "login", lockID, 10, 1800); lerr != nil {
			_ = lerr
		}
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	_ = s.st.ResetFail(r.Context(), "login", lockID)
	tok, err := signSession(s.sessionKey, a.Username, a.PassEpoch, s.st.Now().Unix()+sessionTTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok, "username": a.Username, "role": a.Role,
	})
}

// me 当前会话身份(客户端启动时校验令牌存活+取角色)。
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	a := principalOf(r).Admin
	writeJSON(w, http.StatusOK, map[string]any{"username": a.Username, "role": a.Role})
}

// logout 服务端吊销:bump 本人口令纪元,全部在途会话即刻失效。
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	a := principalOf(r).Admin
	ok, err := s.st.CASAccount(r.Context(), a, func(n *ddbstore.Account) {
		n.PassEpoch++
		n.UpdatedAt = s.st.Now().Unix()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "cas conflict")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// changePassword 改本人密码(须验旧密);成功即 bump 纪元吊销旧会话。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.OldPassword == "" {
		writeErr(w, http.StatusBadRequest, "old_password and new_password required")
		return
	}
	if msg := ddbstore.ValidatePassword(in.NewPassword); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	a := principalOf(r).Admin
	if !ddbstore.VerifyPassword(in.OldPassword, a.PassHash) {
		writeErr(w, http.StatusForbidden, "原密码错误")
		return
	}
	hash, err := ddbstore.HashPasswordNew(in.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok, err := s.st.CASAccount(r.Context(), a, func(n *ddbstore.Account) {
		n.PassHash = hash
		n.PassEpoch++
		n.Version++
		n.UpdatedAt = s.st.Now().Unix()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "cas conflict")
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: a.Username, Action: "change_password", Target: a.Username, OK: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listAccounts root 专属:全量管理员(PassHash 永不下发)。
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.st.ListAccounts(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

// createAccount root 专属:建普通管理员(API 不可建 root)。
func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "username and password required")
		return
	}
	if msg := ddbstore.ValidateUsername(in.Username); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if msg := ddbstore.ValidatePassword(in.Password); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := ddbstore.HashPasswordNew(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	epoch, err := ddbstore.NewPassEpoch()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := principalOf(r).Admin.Username
	now := s.st.Now().Unix()
	ok, err := s.st.CreateAccount(r.Context(), &ddbstore.Account{
		Username: in.Username, Role: ddbstore.RoleAdmin, PassHash: hash,
		Status: ddbstore.UserActive, PassEpoch: epoch, Version: 1,
		CreatedAt: now, CreatedBy: actor, UpdatedAt: now,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "用户名已存在")
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: actor, Action: "create_account", Target: in.Username, OK: true,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// accountOp root 改他人账号的公共骨架:读目标 → 拒改 root → 版本校验 → mutate。
// version 为乐观锁基线(客户端从列表取),不符即 409。
func (s *Server) accountOp(w http.ResponseWriter, r *http.Request, version int64,
	mutate func(*ddbstore.Account)) (*ddbstore.Account, bool) {
	name := r.PathValue("u")
	a, err := s.st.GetAccount(r.Context(), name)
	if errors.Is(err, ddbstore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "account not found")
		return nil, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if a.Role == ddbstore.RoleRoot {
		writeErr(w, http.StatusBadRequest, "root 账号不可经接口改动")
		return nil, false
	}
	if a.Version != version {
		writeErr(w, http.StatusConflict, "version mismatch")
		return nil, false
	}
	ok, err := s.st.CASAccount(r.Context(), a, func(n *ddbstore.Account) {
		mutate(n)
		n.Version++
		n.UpdatedAt = s.st.Now().Unix()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if !ok {
		writeErr(w, http.StatusConflict, "cas conflict")
		return nil, false
	}
	return a, true
}

// setAccountStatus root 专属:禁用/启用管理员。禁用即 bump 纪元吊销其会话。
func (s *Server) setAccountStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil ||
		(in.Status != ddbstore.UserActive && in.Status != ddbstore.UserDisabled) {
		writeErr(w, http.StatusBadRequest, "status(active|disabled) and version required")
		return
	}
	a, ok := s.accountOp(w, r, in.Version, func(n *ddbstore.Account) {
		n.Status = in.Status
		n.PassEpoch++
	})
	if !ok {
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: principalOf(r).Admin.Username, Action: "status_" + in.Status, Target: a.Username, OK: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resetAccountPassword root 专属:重置管理员密码,旧会话随之失效。
func (s *Server) resetAccountPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Version  int64  `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "password and version required")
		return
	}
	if msg := ddbstore.ValidatePassword(in.Password); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := ddbstore.HashPasswordNew(in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	a, ok := s.accountOp(w, r, in.Version, func(n *ddbstore.Account) {
		n.PassHash = hash
		n.PassEpoch++
	})
	if !ok {
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: principalOf(r).Admin.Username, Action: "reset_password", Target: a.Username, OK: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteAccount root 专属:硬删(条件删除,基线=读取时整值)。root 不可删。
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "version required")
		return
	}
	name := r.PathValue("u")
	a, err := s.st.GetAccount(r.Context(), name)
	if errors.Is(err, ddbstore.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if a.Role == ddbstore.RoleRoot {
		writeErr(w, http.StatusBadRequest, "root 账号不可删除")
		return
	}
	if a.Version != in.Version {
		writeErr(w, http.StatusConflict, "version mismatch")
		return
	}
	ok, err := s.st.DeleteAccount(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusConflict, "cas conflict")
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: principalOf(r).Admin.Username, Action: "delete_account", Target: name, OK: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
