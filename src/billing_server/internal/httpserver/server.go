package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"fengshen-desubber/billing_server/internal/billing"
	"fengshen-desubber/billing_server/internal/cardkey"
	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
)

type ctxKey int

const ctxPrincipal ctxKey = 0

// principal 调用主体：Admin（acct: 键族账户，会话令牌）或 Card（卡面即凭证）。
type principal struct {
	Admin *ddbstore.Account
	Card  *ddbstore.Card
}

type Server struct {
	st         *ddbstore.Store
	reg        *provider.Registry
	sessionKey []byte
}

func New(st *ddbstore.Store, reg *provider.Registry, sessionKey []byte) *Server {
	return &Server{st: st, reg: reg, sessionKey: sessionKey}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// 任务:TOS 直传协议——建单发预签名 PUT、客户端直传、submit 探测扣点提交算子、
	// GET 轮询内联推进算子状态、download 302 到算子侧成片地址。
	mux.Handle("POST /v1/tasks", s.auth("", s.createTask))
	mux.Handle("POST /v1/tasks/{id}/submit", s.auth("", s.submitTask))
	mux.Handle("GET /v1/tasks/{id}", s.auth("", s.getTask))
	mux.Handle("GET /v1/tasks/{id}/download", s.auth("", s.download))
	mux.Handle("GET /v1/balance", s.auth("", s.balance))
	// 激活（卡面 + 机器码绑定）：不走 auth 中间件，自行解析卡面
	mux.Handle("POST /v1/activate", http.HandlerFunc(s.activate))
	// 管理员账号与会话：login 公开，me/logout/password 两角色，accounts* 仅 root
	mux.Handle("POST /v1/admin/login", http.HandlerFunc(s.login))
	mux.Handle("GET /v1/admin/me", s.auth(ddbstore.RoleAdmin, s.me))
	mux.Handle("POST /v1/admin/logout", s.auth(ddbstore.RoleAdmin, s.logout))
	mux.Handle("POST /v1/admin/password", s.auth(ddbstore.RoleAdmin, s.changePassword))
	mux.Handle("GET /v1/admin/accounts", s.auth(ddbstore.RoleRoot, s.listAccounts))
	mux.Handle("POST /v1/admin/accounts", s.auth(ddbstore.RoleRoot, s.createAccount))
	mux.Handle("POST /v1/admin/accounts/{u}/status", s.auth(ddbstore.RoleRoot, s.setAccountStatus))
	mux.Handle("POST /v1/admin/accounts/{u}/password", s.auth(ddbstore.RoleRoot, s.resetAccountPassword))
	mux.Handle("POST /v1/admin/accounts/{u}/delete", s.auth(ddbstore.RoleRoot, s.deleteAccount))
	// 业务管理（root/admin 皆可）
	mux.Handle("POST /v1/admin/users", s.auth(ddbstore.RoleAdmin, s.createUser))
	mux.Handle("POST /v1/admin/credits", s.auth(ddbstore.RoleAdmin, s.grant))
	mux.Handle("GET /v1/admin/transactions", s.auth(ddbstore.RoleAdmin, s.transactions))
	// 卡密（root/admin 皆可）
	mux.Handle("POST /v1/admin/cards/generate", s.auth(ddbstore.RoleAdmin, s.generateCards))
	mux.Handle("POST /v1/admin/cards/recharge", s.auth(ddbstore.RoleAdmin, s.rechargeCard))
	mux.Handle("POST /v1/admin/cards/revoke", s.auth(ddbstore.RoleAdmin, s.revokeCard))
	mux.Handle("POST /v1/admin/cards/unrevoke", s.auth(ddbstore.RoleAdmin, s.unrevokeCard))
	mux.Handle("POST /v1/admin/cards/unbind", s.auth(ddbstore.RoleAdmin, s.unbindCard))
	mux.Handle("GET /v1/admin/cards/audit", s.auth(ddbstore.RoleAdmin, s.cardAudit))
	mux.Handle("GET /v1/admin/cards", s.auth(ddbstore.RoleAdmin, s.listCards))
	mux.Handle("GET /v1/cards/{card}/status", s.auth("", s.cardStatus))
	return mux
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// auth 解析 Bearer：含 '.' 按会话令牌（HMAC 验签 + 账号/纪元/到期核对），
// 否则按卡面查卡。role 为准入闸：""=卡或管理员皆可，"admin"=任一管理员，
// "root"=仅超级管理员。
// 卡面撞库防护：卡路径 auth 连败 10 次锁 30 分钟（按来源 IP）；
// 会话验签失败不计入（HMAC 不可爆破）。机器码校验失败亦不计（卡是真的）。
func (s *Server) auth(role string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			writeErr(w, http.StatusUnauthorized, "missing token")
			return
		}
		var p *principal
		if strings.Contains(tok, ".") {
			a, code, msg := s.resolveSession(r, tok)
			if a == nil {
				writeErr(w, code, msg)
				return
			}
			p = &principal{Admin: a}
		} else {
			ip := clientIP(r)
			if rem, err := s.st.LockedFor(r.Context(), "auth", ip); err == nil && rem > 0 {
				writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("locked, retry in %ds", rem))
				return
			}
			c, err := s.st.GetCardByCode(r.Context(), tok)
			if errors.Is(err, ddbstore.ErrCardNotFound) || errors.Is(err, cardkey.ErrNoPepper) {
				if _, lerr := s.st.FailLock(r.Context(), "auth", ip, 10, 1800); lerr != nil {
					_ = lerr
				}
				writeErr(w, http.StatusUnauthorized, "invalid token")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			if c.Status == ddbstore.CardRevoked {
				writeErr(w, http.StatusForbidden, "card_revoked")
				return
			}
			if c.Status != ddbstore.CardRedeemed && c.Status != ddbstore.CardActive {
				writeErr(w, http.StatusForbidden, "card_not_activated")
				return
			}
			mh := r.Header.Get("X-Machine-Hash")
			if mh == "" || mh != c.MachineHash {
				writeErr(w, http.StatusForbidden, "machine_mismatch")
				return
			}
			if c.Status != ddbstore.CardRedeemed || !c.Credited {
				// 旧模型 active 卡/迁移半成品：懒迁移——点数转机器账户并置 redeemed
				if _, err := s.st.EnsureRedeemed(r.Context(), c, mh); err != nil {
					writeErr(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			_ = s.st.ResetFail(r.Context(), "auth", ip)
			p = &principal{Card: c}
		}

		switch role {
		case ddbstore.RoleRoot:
			if p.Admin == nil || p.Admin.Role != ddbstore.RoleRoot {
				writeErr(w, http.StatusForbidden, "root only")
				return
			}
		case ddbstore.RoleAdmin:
			if p.Admin == nil {
				writeErr(w, http.StatusForbidden, "admin only")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	})
}

// resolveSession 验签并核对账号：签名/形态非法、账号不存在、纪元不符、
// 已到期 → 401；账号被禁用 → 403。
func (s *Server) resolveSession(r *http.Request, tok string) (*ddbstore.Account, int, string) {
	pl, ok := verifySession(s.sessionKey, tok)
	if !ok {
		return nil, http.StatusUnauthorized, "invalid token"
	}
	a, err := s.st.GetAccount(r.Context(), pl.U)
	if errors.Is(err, ddbstore.ErrNotFound) {
		return nil, http.StatusUnauthorized, "invalid token"
	}
	if err != nil {
		return nil, http.StatusInternalServerError, err.Error()
	}
	if a.Status != ddbstore.UserActive {
		return nil, http.StatusForbidden, "account disabled"
	}
	if pl.Epoch != a.PassEpoch || pl.Exp <= s.st.Now().Unix() {
		return nil, http.StatusUnauthorized, "invalid token"
	}
	return a, 0, ""
}

func principalOf(r *http.Request) *principal {
	return r.Context().Value(ctxPrincipal).(*principal)
}

// cardOf 取卡主体；admin token 调用户态接口返回 nil。
func cardOf(r *http.Request) *ddbstore.Card { return principalOf(r).Card }

// validMachineHash 机器码格式：64 位小写 hex（SHA-256 十六进制形态）。
func validMachineHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// activate 卡激活=充值券核销：Bearer 卡面 + X-Machine-Hash（64 位小写 hex）。
// revoked→403 card_revoked；已绑异机→409 card_bound_other；
// 其余（inactive 新卡 / 旧模型 active 卡 / redeemed 同机幂等）走 EnsureRedeemed：
// 卡面点数转入机器账户并置 redeemed，返回机器累计余额（credits）。
// 与 auth 中间件同规：无效卡面计入 IP 连败锁，机器码问题不计。
func (s *Server) activate(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "missing token")
		return
	}
	ip := clientIP(r)
	if rem, err := s.st.LockedFor(r.Context(), "auth", ip); err == nil && rem > 0 {
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf("locked, retry in %ds", rem))
		return
	}
	machine := r.Header.Get("X-Machine-Hash")
	if !validMachineHash(machine) {
		writeErr(w, http.StatusBadRequest, "invalid_machine_hash")
		return
	}
	c, err := s.st.GetCardByCode(r.Context(), tok)
	if errors.Is(err, ddbstore.ErrCardNotFound) || errors.Is(err, cardkey.ErrNoPepper) {
		if _, lerr := s.st.FailLock(r.Context(), "auth", ip, 10, 1800); lerr != nil {
			_ = lerr
		}
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.st.ResetFail(r.Context(), "auth", ip)

	if c.Status == ddbstore.CardRevoked {
		writeErr(w, http.StatusForbidden, "card_revoked")
		return
	}
	if c.MachineHash != "" && c.MachineHash != machine {
		writeErr(w, http.StatusConflict, "card_bound_other")
		return
	}
	balance, err := s.st.EnsureRedeemed(r.Context(), c, machine)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credits": balance, "machine_hash": machine, "status": ddbstore.CardRedeemed,
	})
}

const (
	uploadURLExpireSec = 7200             // 预签名 PUT/GET 有效期(客户端直传与算子回源)
	pollTimeout        = 30 * time.Minute // processing 超过此时长判超时失败并退款
)

// createTask 建单(不读 body、不扣点):登记 uploading 任务并发预签名 PUT URL,
// 客户端直传原片到 TOS 后调 submit。上传失败/永不提交的对象由桶生命周期自动过期。
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	c := cardOf(r)
	if c == nil {
		writeErr(w, http.StatusForbidden, "需要卡号调用")
		return
	}
	filename := path.Base(strings.ReplaceAll(strings.TrimSpace(r.Header.Get("X-Video-Filename")), "\\", "/"))
	ext := strings.ToLower(path.Ext(filename))
	if (ext != ".mp4" && ext != ".mov") || filename == "." {
		writeErr(w, http.StatusBadRequest, "only mp4/mov supported")
		return
	}

	// 平台路由:X-Provider 头缺省走默认平台,未知名拒绝。
	providerName := r.Header.Get("X-Provider")
	if providerName == "" {
		providerName = s.reg.DefaultName()
	} else if _, ok := s.reg.Get(providerName); !ok {
		writeErr(w, http.StatusBadRequest, "unknown_provider")
		return
	}
	// 注册表无此平台(Lambda 形态未配置 TOS/LAS env)时,建单只会产生无人处理的
	// 孤儿任务,必须在登记之前拒绝。
	p, ok := s.reg.Get(providerName)
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "在线任务暂未部署(服务端无算子平台)")
		return
	}

	taskID := uuid.NewString()
	srcKey := fmt.Sprintf("input/%s%s", taskID, ext)
	t := &ddbstore.Task{
		ID: taskID, CardID: c.ID, Provider: providerName, SrcKey: srcKey,
		OriginalFilename: filename,
	}
	if err := s.st.CreateUploadingTask(r.Context(), t); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	uploadURL, err := p.Uploader.PresignPut(r.Context(), srcKey, uploadURLExpireSec)
	if err != nil {
		_ = s.st.FailTaskWithRefund(r.Context(), taskID, "presign put: "+err.Error())
		writeErr(w, http.StatusBadGateway, "presign put: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task_id": taskID, "upload_url": uploadURL, "expires_in": uploadURLExpireSec,
	})
}

// submitTask 客户端直传完成后提交算子。时长与扣点在 LAS 完成后按权威结果结算。
func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.ownTask(w, r)
	if !ok {
		return
	}
	if t.Status != ddbstore.TaskUploading {
		writeErr(w, http.StatusConflict, "task not submittable: "+t.Status)
		return
	}
	p, pok := s.reg.Get(t.Provider)
	if !pok {
		writeErr(w, http.StatusServiceUnavailable, "在线任务暂未部署(服务端无算子平台)")
		return
	}
	getURL, err := p.Uploader.PresignGet(r.Context(), t.SrcKey, uploadURLExpireSec)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "presign get: "+err.Error())
		return
	}
	balance, err := s.st.StartTask(r.Context(), t.ID)
	if errors.Is(err, ddbstore.ErrInsufficientBalance) {
		writeErr(w, http.StatusPaymentRequired, "insufficient balance: need at least 1 credit")
		return
	}
	if errors.Is(err, ddbstore.ErrTaskState) {
		writeErr(w, http.StatusConflict, "task not submittable")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	lasID, err := p.Operator.Submit(r.Context(), getURL, t.ID)
	if err != nil {
		s.cleanupTOS(r, p, t)
		if ferr := s.st.FailTaskWithRefund(r.Context(), t.ID, "las submit: "+err.Error()); ferr != nil {
			log.Printf("task %s fail: %v", t.ID, ferr)
		}
		writeErr(w, http.StatusBadGateway, "las submit: "+err.Error())
		return
	}
	if err := s.st.SetLasTaskID(r.Context(), t.ID, lasID); err != nil {
		log.Printf("task %s set las id: %v", t.ID, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": t.ID, "duration_sec": 0, "cost": 0, "balance": balance,
	})
}

func (s *Server) taskVisible(r *http.Request, t *ddbstore.Task) bool {
	p := principalOf(r)
	if p.Admin != nil {
		return true
	}
	return p.Card != nil && t.CardID == p.Card.ID
}

// ownTask 取任务并校验可见性(卡主本人或管理员);不可见一律 404。
func (s *Server) ownTask(w http.ResponseWriter, r *http.Request) (*ddbstore.Task, bool) {
	t, err := s.st.GetTask(r.Context(), r.PathValue("id"))
	if errors.Is(err, ddbstore.ErrNotFound) || (err == nil && !s.taskVisible(r, t)) {
		writeErr(w, http.StatusNotFound, "task not found")
		return nil, false
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return t, true
}

// cleanupTOS 删除任务的 TOS 临时输入视频;失败仅记日志(桶生命周期兜底)。
func (s *Server) cleanupTOS(r *http.Request, p provider.Provider, t *ddbstore.Task) {
	if t.SrcKey == "" {
		return
	}
	if err := p.Uploader.Delete(r.Context(), t.SrcKey); err != nil {
		log.Printf("task %s delete tos %s: %v", t.ID, t.SrcKey, err)
	}
}

// advanceTask processing 态内联推进:轮询算子,COMPLETED 记录成片 URL、
// FAILED/超时退款;两路径均清理 TOS 临时视频。返回最新任务。
func (s *Server) advanceTask(r *http.Request, t *ddbstore.Task) *ddbstore.Task {
	p, ok := s.reg.Get(t.Provider)
	if !ok {
		return t // 平台被摘除:保持 processing,待人工处置
	}
	status, videoURL, errMsg, duration, err := p.Operator.Poll(r.Context(), t.LasTaskID)
	if err != nil {
		log.Printf("task %s poll error: %v", t.ID, err)
		return t // 算子侧抖动:维持 processing,下一轮再试
	}
	switch {
	case status == "COMPLETED" && videoURL != "" && duration > 0:
		dur := int64(math.Ceil(duration))
		cost := billing.Cost(dur)
		if _, err := s.st.FinalizeTaskWithDebit(r.Context(), t.ID, dur, cost, videoURL); err != nil &&
			!errors.Is(err, ddbstore.ErrInsufficientBalance) {
			log.Printf("task %s settle: %v", t.ID, err)
		}
		s.cleanupTOS(r, p, t)
	case status == "FAILED":
		s.cleanupTOS(r, p, t)
		if err := s.st.FailTaskWithRefund(r.Context(), t.ID, "las failed: "+errMsg); err != nil {
			log.Printf("task %s fail: %v", t.ID, err)
		}
	case time.Since(time.Unix(t.UpdatedAt, 0)) > pollTimeout:
		s.cleanupTOS(r, p, t)
		if err := s.st.FailTaskWithRefund(r.Context(), t.ID, "poll timeout"); err != nil {
			log.Printf("task %s timeout-fail: %v", t.ID, err)
		}
	}
	if fresh, err := s.st.GetTask(r.Context(), t.ID); err == nil {
		return fresh
	}
	return t
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.ownTask(w, r)
	if !ok {
		return
	}
	if t.Status == ddbstore.TaskProcessing && t.LasTaskID != "" {
		t = s.advanceTask(r, t)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": t.ID, "status": t.Status, "duration_sec": t.DurationSec,
		"cost": t.Cost, "error": t.Error, "original_filename": t.OriginalFilename,
	})
}

// download 完成态 302 到算子侧成片地址(重新轮询取新 URL 防过期,失败回落库存 URL)。
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	t, ok := s.ownTask(w, r)
	if !ok {
		return
	}
	if t.Status != ddbstore.TaskCompleted {
		writeErr(w, http.StatusConflict, "task not completed: "+t.Status)
		return
	}
	url := t.ResultURL
	if p, pok := s.reg.Get(t.Provider); pok && t.LasTaskID != "" {
		if status, videoURL, _, _, err := p.Operator.Poll(r.Context(), t.LasTaskID); err == nil &&
			status == "COMPLETED" && videoURL != "" {
			url = videoURL
		}
	}
	if url == "" {
		writeErr(w, http.StatusBadGateway, "result url unavailable")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	c := cardOf(r)
	if c == nil {
		writeErr(w, http.StatusForbidden, "需要卡号调用")
		return
	}
	m, err := s.st.GetMachine(r.Context(), c.MachineHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credits": m.Balance})
}

// createUser 旧协议兼容：建卡即建账号，token 字段返回明文卡面（仅此一次）。
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	code, err := cardkey.Generate()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	c, err := s.st.CreateCard(r.Context(), in.Name, code, 0, "")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": c.ID, "name": c.Name, "token": code})
}

// grant 旧协议兼容：按卡 id 给其绑定机器充值（卡须已核销；点数记机器账户）。
func (s *Server) grant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"user_id"`
		Amount int64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Amount <= 0 || in.UserID == 0 {
		writeErr(w, http.StatusBadRequest, "user_id and positive amount required")
		return
	}
	c, err := s.st.GetCardByID(r.Context(), in.UserID)
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusBadRequest, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Status != ddbstore.CardRedeemed || c.MachineHash == "" {
		writeErr(w, http.StatusConflict, "card_not_redeemed")
		return
	}
	balance, err := s.st.CreditMachine(r.Context(), c.MachineHash, in.Amount, "grant", "by-admin", c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{Actor: "admin", Action: "recharge",
		Target: c.CodeMasked, Detail: fmt.Sprintf("+%d machine=%s", in.Amount, c.MachineHash[:12]), OK: true})
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance})
}

// transactions 按卡 id 查其绑定机器的资金流水（旧协议 user_id 形参沿用卡 id）。
func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	var cardID int64
	fmt.Sscanf(r.URL.Query().Get("user_id"), "%d", &cardID)
	if cardID == 0 {
		writeErr(w, http.StatusBadRequest, "user_id required")
		return
	}
	c, err := s.st.GetCardByID(r.Context(), cardID)
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusBadRequest, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	txs, err := s.st.ListMachineTx(r.Context(), c.MachineHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range txs {
		if txs[i].Kind != "debit" && txs[i].Kind != "refund" {
			continue
		}
		if task, err := s.st.GetTask(r.Context(), txs[i].TaskID); err == nil {
			txs[i].OriginalFilename = task.OriginalFilename
		}
	}
	if txs == nil {
		txs = []ddbstore.CreditTx{}
	}
	writeJSON(w, http.StatusOK, txs)
}
