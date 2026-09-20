package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"fengshen-desubber/billing_server/internal/cardkey"
	"fengshen-desubber/billing_server/internal/ddbstore"
)

// generateCards 批量发卡（明文仅此一次返回）。
func (s *Server) generateCards(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Count   int    `json:"count"`
		Credits int64  `json:"credits"`
		Batch   string `json:"batch"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Count <= 0 || in.Count > 1000 || in.Credits < 0 {
		writeErr(w, http.StatusBadRequest, "1<=count<=1000 and credits>=0 required")
		return
	}
	type issued struct {
		ID      int64  `json:"id"`
		Code    string `json:"code"`
		Name    string `json:"name"`
		Batch   string `json:"batch"`
		Credits int64  `json:"credits"`
	}
	out := make([]issued, 0, in.Count)
	for i := 0; i < in.Count; i++ {
		code, err := cardkey.Generate()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		name := in.Name
		if name == "" {
			name = fmt.Sprintf("card-%s", code[14:])
		}
		c, err := s.st.CreateCard(r.Context(), name, code, in.Credits, in.Batch)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, issued{ID: c.ID, Code: code, Name: c.Name, Batch: c.Batch, Credits: c.Balance})
	}
	_ = s.st.AppendAudit(r.Context(), ddbstore.AuditEntry{
		Actor: "admin", Action: "generate",
		Target: in.Batch, Detail: fmt.Sprintf("count=%d credits=%d", in.Count, in.Credits), OK: true,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"cards": out})
}

// rechargeCard 按卡面充值（只能给卡号加点数）。
func (s *Server) rechargeCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Card    string `json:"card"`
		Credits int64  `json:"credits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Card == "" || in.Credits <= 0 {
		writeErr(w, http.StatusBadRequest, "card and positive credits required")
		return
	}
	c, err := s.st.GetCardByCode(r.Context(), in.Card)
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	balance, err := s.st.RechargeCard(r.Context(), c.ID, in.Credits, "by-code")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"balance": balance})
}

// setCardStatus 吊销/恢复共用：{card} 单卡或 {batch} 整批。
func (s *Server) setCardStatus(w http.ResponseWriter, r *http.Request, status string) {
	var in struct {
		Card  string `json:"card"`
		Batch string `json:"batch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.Card == "" && in.Batch == "") {
		writeErr(w, http.StatusBadRequest, "card or batch required")
		return
	}
	if in.Batch != "" {
		n, err := s.st.RevokeBatch(r.Context(), in.Batch, status, "admin")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": n})
		return
	}
	c, err := s.st.GetCardByCode(r.Context(), in.Card)
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetCardStatus(r.Context(), c.Hash, status, "admin"); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) revokeCard(w http.ResponseWriter, r *http.Request) {
	s.setCardStatus(w, r, ddbstore.CardRevoked)
}

// unrevokeCard 恢复吊销：一律落 inactive（绑定已失效，须重新激活）。
func (s *Server) unrevokeCard(w http.ResponseWriter, r *http.Request) {
	s.setCardStatus(w, r, ddbstore.CardInactive)
}

// unbindCard 解绑机器码：{"card_id": n} 或 {"code": "卡面"}。
// 清 MachineHash/BoundAt、status→inactive（revoked 卡报 400），写审计 unbind。
func (s *Server) unbindCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CardID int64  `json:"card_id"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.CardID == 0 && in.Code == "") {
		writeErr(w, http.StatusBadRequest, "card_id or code required")
		return
	}
	var c *ddbstore.Card
	var err error
	if in.CardID != 0 {
		c, err = s.st.GetCardByID(r.Context(), in.CardID)
	} else {
		c, err = s.st.GetCardByCode(r.Context(), in.Code)
	}
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.UnbindCard(r.Context(), c.Hash, "admin"); errors.Is(err, ddbstore.ErrCardRevoked) {
		writeErr(w, http.StatusBadRequest, "card revoked")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listCards 卡列表（hash 脱敏，不含明文卡面）。
func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	cards, err := s.st.ListCards(r.Context(), r.URL.Query().Get("batch"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type view struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		CodeMasked string `json:"code_masked"`
		Batch      string `json:"batch"`
		Balance    int64  `json:"balance"`
		Status     string `json:"status"`
		CreatedAt  int64  `json:"created_at"`
	}
	out := make([]view, 0, len(cards))
	for _, c := range cards {
		out = append(out, view{c.ID, c.Name, c.CodeMasked, c.Batch, c.Balance, c.Status, c.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out})
}

func (s *Server) cardAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.st.AuditList(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}

// cardStatus 卡自查状态（不消耗）：卡面路径须与 Bearer 一致，或 admin；限频 10/min/IP。
func (s *Server) cardStatus(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	n, err := s.st.HitRate(r.Context(), "cardstatus", ip, 60)
	if err == nil && n > 10 {
		writeErr(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	code := r.PathValue("card")
	p := principalOf(r)
	c, err := s.st.GetCardByCode(r.Context(), code)
	if errors.Is(err, ddbstore.ErrCardNotFound) {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p.Admin == nil && (p.Card == nil || p.Card.Hash != c.Hash) {
		writeErr(w, http.StatusForbidden, "只能查本人卡")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": c.Status, "balance": c.Balance, "name": c.Name, "batch": c.Batch,
	})
}
