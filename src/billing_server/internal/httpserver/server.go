package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"fengshen-desubber/billing_server/internal/billing"
	"fengshen-desubber/billing_server/internal/cardkey"
	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/probe"
	"fengshen-desubber/billing_server/internal/provider"
)

type ctxKey int

const ctxPrincipal ctxKey = 0

// principal 调用主体：Admin（user: 键族 token）或 Card（卡面即凭证）。
type principal struct {
	Admin *ddbstore.User
	Card  *ddbstore.Card
}

type Server struct {
	st     *ddbstore.Store
	srcDir string
	reg    *provider.Registry
}

func New(st *ddbstore.Store, srcDir string, reg *provider.Registry) *Server {
	return &Server{st: st, srcDir: srcDir, reg: reg}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// 冻结协议：路径/方法/请求响应字段不变
	mux.Handle("POST /v1/tasks", s.auth(false, s.createTask))
	mux.Handle("GET /v1/tasks/{id}", s.auth(false, s.getTask))
	mux.Handle("GET /v1/tasks/{id}/download", s.auth(false, s.download))
	mux.Handle("GET /v1/balance", s.auth(false, s.balance))
	// 激活（卡面 + 机器码绑定）：不走 auth 中间件，自行解析卡面
	mux.Handle("POST /v1/activate", http.HandlerFunc(s.activate))
	mux.Handle("POST /v1/admin/users", s.auth(true, s.createUser))
	mux.Handle("POST /v1/admin/credits", s.auth(true, s.grant))
	mux.Handle("GET /v1/admin/transactions", s.auth(true, s.transactions))
	// 卡密（新增，不动旧路由）
	mux.Handle("POST /v1/admin/cards/generate", s.auth(true, s.generateCards))
	mux.Handle("POST /v1/admin/cards/recharge", s.auth(true, s.rechargeCard))
	mux.Handle("POST /v1/admin/cards/revoke", s.auth(true, s.revokeCard))
	mux.Handle("POST /v1/admin/cards/unrevoke", s.auth(true, s.unrevokeCard))
	mux.Handle("POST /v1/admin/cards/unbind", s.auth(true, s.unbindCard))
	mux.Handle("GET /v1/admin/cards/audit", s.auth(true, s.cardAudit))
	mux.Handle("GET /v1/admin/cards", s.auth(true, s.listCards))
	mux.Handle("GET /v1/cards/{card}/status", s.auth(false, s.cardStatus))
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

// auth 解析 Bearer：admin token（user: 键族）优先，否则按卡面查卡。
// 卡面撞库防护：auth 连败 10 次锁 30 分钟（按来源 IP）。
// 卡身份附加机器码校验：卡须 active 且 X-Machine-Hash 等于卡上绑定值；
// 机器码校验失败不计入 auth 连败锁（卡是真的）。
func (s *Server) auth(adminOnly bool, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		var p *principal
		if u, err := s.st.GetUserByToken(r.Context(), tok); err == nil {
			p = &principal{Admin: u}
		} else if !errors.Is(err, ddbstore.ErrNotFound) {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if p == nil {
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
			if c.Status != ddbstore.CardActive {
				writeErr(w, http.StatusForbidden, "card_not_activated")
				return
			}
			if mh := r.Header.Get("X-Machine-Hash"); mh == "" || mh != c.MachineHash {
				writeErr(w, http.StatusForbidden, "machine_mismatch")
				return
			}
			p = &principal{Card: c}
		}
		_ = s.st.ResetFail(r.Context(), "auth", ip)

		if adminOnly && (p.Admin == nil || !p.Admin.IsAdmin) {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, p)))
	})
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

// activate 卡激活：Bearer 卡面 + X-Machine-Hash（64 位小写 hex）。
// revoked→403 card_revoked；active 同机→200 幂等；active 异机→409 card_bound_other；
// inactive→CAS 绑定（status→active + MachineHash + BoundAt），写审计 bind。
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

	switch c.Status {
	case ddbstore.CardRevoked:
		writeErr(w, http.StatusForbidden, "card_revoked")
		return
	case ddbstore.CardActive:
		if c.MachineHash != machine {
			writeErr(w, http.StatusConflict, "card_bound_other")
			return
		}
		// 同机幂等
	default: // inactive：CAS 绑定激活
		if err := s.st.BindCard(r.Context(), c, machine); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credits": c.Balance, "machine_hash": c.MachineHash, "status": c.Status,
	})
}

const maxUpload = 2 << 30 // 2GB

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	c := cardOf(r)
	if c == nil {
		writeErr(w, http.StatusForbidden, "需要卡号调用")
		return
	}
	filename := filepath.Base(r.Header.Get("X-Video-Filename"))
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".mp4" && ext != ".mov" {
		writeErr(w, http.StatusBadRequest, "only mp4/mov supported")
		return
	}

	// 平台路由：X-Provider 头缺省走默认平台，未知名拒绝。
	providerName := r.Header.Get("X-Provider")
	if providerName == "" {
		providerName = s.reg.DefaultName()
	} else if _, ok := s.reg.Get(providerName); !ok {
		writeErr(w, http.StatusBadRequest, "unknown_provider")
		return
	}

	taskID := uuid.NewString()
	srcPath := filepath.Join(s.srcDir, taskID+ext)
	f, err := os.Create(srcPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, copyErr := io.Copy(f, http.MaxBytesReader(w, r.Body, maxUpload))
	f.Close()
	if copyErr != nil {
		os.Remove(srcPath)
		writeErr(w, http.StatusBadRequest, "upload failed: "+copyErr.Error())
		return
	}

	dur, err := probe.DurationSeconds(srcPath)
	if err != nil {
		os.Remove(srcPath)
		writeErr(w, http.StatusBadRequest, "probe duration: "+err.Error())
		return
	}
	cost := billing.Cost(dur)

	t := &ddbstore.Task{
		ID: taskID, CardID: c.ID, Provider: providerName, SrcPath: srcPath,
		DurationSec: dur, Cost: cost,
	}
	balanceAfter, err := s.st.CreateTaskWithDebit(r.Context(), t)
	if errors.Is(err, ddbstore.ErrInsufficientBalance) {
		os.Remove(srcPath)
		writeErr(w, http.StatusPaymentRequired,
			fmt.Sprintf("insufficient balance: need %d, have %d", cost, balanceAfter))
		return
	}
	if errors.Is(err, ddbstore.ErrCardRevoked) {
		os.Remove(srcPath)
		writeErr(w, http.StatusForbidden, "card revoked")
		return
	}
	if err != nil {
		os.Remove(srcPath)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"task_id": taskID, "duration_sec": dur, "cost": cost, "balance": balanceAfter,
	})
}

func (s *Server) taskVisible(r *http.Request, t *ddbstore.Task) bool {
	p := principalOf(r)
	if p.Admin != nil && p.Admin.IsAdmin {
		return true
	}
	return p.Card != nil && t.CardID == p.Card.ID
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.GetTask(r.Context(), r.PathValue("id"))
	if errors.Is(err, ddbstore.ErrNotFound) || (err == nil && !s.taskVisible(r, t)) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": t.ID, "status": t.Status, "duration_sec": t.DurationSec,
		"cost": t.Cost, "error": t.Error,
	})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.GetTask(r.Context(), r.PathValue("id"))
	if errors.Is(err, ddbstore.ErrNotFound) || (err == nil && !s.taskVisible(r, t)) {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t.Status != ddbstore.TaskCompleted {
		writeErr(w, http.StatusConflict, "task not completed: "+t.Status)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.mp4"`, t.ID))
	http.ServeFile(w, r, t.ResultPath)
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) {
	c := cardOf(r)
	if c == nil {
		writeErr(w, http.StatusForbidden, "需要卡号调用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credits": c.Balance})
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

// grant 旧协议兼容：按卡 id 充值（只能给卡加点）。
func (s *Server) grant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"user_id"`
		Amount int64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Amount <= 0 || in.UserID == 0 {
		writeErr(w, http.StatusBadRequest, "user_id and positive amount required")
		return
	}
	balance, err := s.st.RechargeCard(r.Context(), in.UserID, in.Amount, "")
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusBadRequest, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance})
}

func (s *Server) transactions(w http.ResponseWriter, r *http.Request) {
	var cardID int64
	fmt.Sscanf(r.URL.Query().Get("user_id"), "%d", &cardID)
	if cardID == 0 {
		writeErr(w, http.StatusBadRequest, "user_id required")
		return
	}
	txs, err := s.st.ListTx(r.Context(), cardID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if txs == nil {
		txs = []ddbstore.CreditTx{}
	}
	writeJSON(w, http.StatusOK, txs)
}
