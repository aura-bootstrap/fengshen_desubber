// Package billing 远端计费服务纯 HTTP 客户端:卡密激活、余额查询、
// 云端去字幕任务(流式上传视频 -> 轮询状态 -> 流式下载成片)。
// 统一头: Authorization: Bearer <卡面>, X-Machine-Hash: <hex64>。
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 超时约定:轻量接口 30s;视频上传/成片下载 30min(长视频)。
const (
	shortTimeout = 30 * time.Second
	longTimeout  = 30 * time.Minute
)

// Client 计费服务客户端(一次激活会话:server + 卡面 + 机器码)。
type Client struct {
	server      string
	cardKey     string
	machineHash string
	short       *http.Client // activate/balance/gettask
	long        *http.Client // 上传/下载
}

// New 构造客户端;server 允许带尾斜杠,内部归一化。
func New(server, cardKey, machineHash string) *Client {
	return &Client{
		server:      strings.TrimRight(strings.TrimSpace(server), "/"),
		cardKey:     cardKey,
		machineHash: machineHash,
		short:       &http.Client{Timeout: shortTimeout},
		long:        &http.Client{Timeout: longTimeout},
	}
}

// ActivateResp POST /v1/activate 成功响应。
type ActivateResp struct {
	Credits     int    `json:"credits"`
	MachineHash string `json:"machine_hash"`
	Status      string `json:"status"`
}

// CreateTaskResp POST /v1/tasks 成功响应(201)。
type CreateTaskResp struct {
	TaskID      string  `json:"task_id"`
	DurationSec float64 `json:"duration_sec"`
	Cost        int     `json:"cost"`
	Balance     int     `json:"balance"`
}

// TaskInfo GET /v1/tasks/{id} 响应;Status: queued/processing/completed/failed。
type TaskInfo struct {
	TaskID      string  `json:"task_id"`
	Status      string  `json:"status"`
	DurationSec float64 `json:"duration_sec"`
	Cost        int     `json:"cost"`
	Error       string  `json:"error"`
}

// newRequest 建带统一鉴权头的请求。
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.server+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cardKey)
	req.Header.Set("X-Machine-Hash", c.machineHash)
	return req, nil
}

// Activate 卡密激活:绑定本机机器码,返回初始余额。
// 错误: 401 ErrCardInvalid / 403 ErrCardRevoked / 409 ErrBoundOther。
func (c *Client) Activate(ctx context.Context) (*ActivateResp, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/activate", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.short.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeErr(resp)
	}
	var out ActivateResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("激活响应解析失败: %v", err)
	}
	return &out, nil
}

// Balance 查询余额。
// 错误: 403 ErrMachineMismatch / ErrCardNotActivated。
func (c *Client) Balance(ctx context.Context) (int, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/balance", nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.short.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, decodeErr(resp)
	}
	var out struct {
		Credits int `json:"credits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("余额响应解析失败: %v", err)
	}
	return out.Credits, nil
}

// ProgressFn 传输进度回调(sent/got 字节数,total 未知时为 0)。
type ProgressFn func(done, total int64)

// progressReader 包装上传流,按读累计回调字节进度。
type progressReader struct {
	r     io.Reader
	total int64
	sent  int64
	fn    ProgressFn
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		if p.fn != nil {
			p.fn(p.sent, p.total)
		}
	}
	return n, err
}

// CreateTask 上传本地视频建云端任务(流式,不整读进内存)。
// provider 非空时带 X-Provider 头;onProgress 非空时回报上传字节进度。
// 错误: 402 *InsufficientBalanceError。
func (c *Client) CreateTask(ctx context.Context, filePath, provider string, onProgress ProgressFn) (*CreateTaskResp, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开待上传视频失败: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	body := io.Reader(f)
	if onProgress != nil {
		body = &progressReader{r: f, total: st.Size(), fn: onProgress}
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/tasks", body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = st.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Video-Filename", filepath.Base(filePath))
	if provider != "" {
		req.Header.Set("X-Provider", provider)
	}
	resp, err := c.long.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, decodeErr(resp)
	}
	var out CreateTaskResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("建单响应解析失败: %v", err)
	}
	return &out, nil
}

// GetTask 查询云端任务状态(轮询用)。
func (c *Client) GetTask(ctx context.Context, taskID string) (*TaskInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.short.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeErr(resp)
	}
	var out TaskInfo
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("任务状态响应解析失败: %v", err)
	}
	return &out, nil
}

// Download 流式下载成片到 dstPath(先写 .part 再 rename,避免半截文件)。
// onProgress 非空时回报下载字节进度(服务端未给 Content-Length 时 total 为 0)。
// 错误: 409 ErrTaskNotReady。
func (c *Client) Download(ctx context.Context, taskID, dstPath string, onProgress ProgressFn) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/tasks/"+taskID+"/download", nil)
	if err != nil {
		return err
	}
	resp, err := c.long.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decodeErr(resp)
	}
	tmp := dstPath + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var src io.Reader = resp.Body
	total := resp.ContentLength // 未知时为 -1,归一成 0
	if total < 0 {
		total = 0
	}
	if onProgress != nil {
		src = &progressReader{r: resp.Body, total: total, fn: onProgress}
	}
	_, copyErr := io.Copy(f, src)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := firstErr(copyErr, syncErr, closeErr); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("成片写盘失败: %v", err)
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("成片落盘失败: %v", err)
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// errBody 服务端错误体(容错解析:code/error/message/need/have 都可选)。
type errBody struct {
	Code    string `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
	Need    int    `json:"need"`
	Have    int    `json:"have"`
}

func (b errBody) msg() string {
	for _, s := range []string{b.Error, b.Message, b.Code} {
		if s != "" {
			return s
		}
	}
	return ""
}

// decodeErr 把非 2xx 响应映射为类型化错误。
// 409 二义:activate 的 card_bound_other 走 code 识别;
// download 的"未完成"不带该 code,兜底映射为 ErrTaskNotReady。
func decodeErr(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var b errBody
	json.Unmarshal(raw, &b) // 容错:非 JSON 体时 b 全零值
	msg := b.msg()
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrCardInvalid
	case http.StatusPaymentRequired:
		return &InsufficientBalanceError{Need: b.Need, Have: b.Have, Msg: msg}
	case http.StatusForbidden:
		switch b.Code {
		case "card_revoked":
			return ErrCardRevoked
		case "machine_mismatch":
			return ErrMachineMismatch
		case "card_not_activated":
			return ErrCardNotActivated
		}
		return &APIError{Status: resp.StatusCode, Code: b.Code, Msg: msg}
	case http.StatusConflict:
		if b.Code == "card_bound_other" {
			return ErrBoundOther
		}
		return ErrTaskNotReady
	}
	return &APIError{Status: resp.StatusCode, Code: b.Code, Msg: msg}
}
