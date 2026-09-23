// Package billing 云端去字幕服务纯 HTTP 客户端:管理员会话登录、
// 云端任务(建单拿预签名 URL -> 原片直传 TOS -> submit 提交算子 ->
// 轮询状态 -> 302 到算子侧地址下载成片)。
// 统一头: Authorization: Bearer <管理员会话>, X-Machine-Hash: <hex64>(TOS 直传不带)。
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
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

// Client 云端任务客户端(一次管理员会话:server + token + 机器码)。
type Client struct {
	server      string
	token       string
	machineHash string
	short       *http.Client
	long        *http.Client
}

// New 构造客户端;server 允许带尾斜杠,内部归一化。
func New(server, token, machineHash string) *Client {
	return &Client{
		server:      strings.TrimRight(strings.TrimSpace(server), "/"),
		token:       token,
		machineHash: machineHash,
		short:       &http.Client{Timeout: shortTimeout},
		long:        &http.Client{Timeout: longTimeout},
	}
}

// LoginResp POST /v1/admin/login 成功响应。
type LoginResp struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Login 管理员账号换取云端内部任务会话。
func Login(ctx context.Context, server, username, password string) (*LoginResp, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("云端服务地址为空")
	}
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/v1/admin/login", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: shortTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeErr(resp)
	}
	var out LoginResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("登录响应解析失败: %v", err)
	}
	if out.Token == "" {
		return nil, fmt.Errorf("登录响应缺 token")
	}
	return &out, nil
}

// CreateTaskResp 任务提交(submit)成功响应。
type CreateTaskResp struct {
	TaskID      string  `json:"task_id"`
	DurationSec float64 `json:"duration_sec"`
	Cost        int     `json:"cost"`
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
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.machineHash != "" {
		req.Header.Set("X-Machine-Hash", c.machineHash)
	}
	return req, nil
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

// CreateTask TOS 直传三段式:① 建单拿预签名 PUT URL(不读 body)→
// ② 原片直传 TOS(视频不经过云端任务服务)→ ③ submit 提交算子。
// provider 非空时带 X-Provider 头;onProgress 非空时回报直传字节进度。
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

	req, err := c.newRequest(ctx, http.MethodPost, "/v1/tasks", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Video-Filename", filepath.Base(filePath))
	if provider != "" {
		req.Header.Set("X-Provider", provider)
	}
	resp, err := c.short.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, decodeErr(resp)
	}
	var created struct {
		TaskID    string `json:"task_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("建单响应解析失败: %v", err)
	}
	if created.TaskID == "" || created.UploadURL == "" {
		return nil, fmt.Errorf("建单响应缺 task_id/upload_url")
	}

	body := io.Reader(f)
	if onProgress != nil {
		body = &progressReader{r: f, total: st.Size(), fn: onProgress}
	}
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, created.UploadURL, body)
	if err != nil {
		return nil, err
	}
	putReq.ContentLength = st.Size()
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putResp, err := c.long.Do(putReq)
	if err != nil {
		return nil, &ObjectStoreError{Cause: err}
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusCreated &&
		putResp.StatusCode != http.StatusNoContent {
		return nil, decodeObjectStoreErr(putResp)
	}

	subReq, err := c.newRequest(ctx, http.MethodPost, "/v1/tasks/"+created.TaskID+"/submit", nil)
	if err != nil {
		return nil, err
	}
	subResp, err := c.long.Do(subReq)
	if err != nil {
		return nil, err
	}
	defer subResp.Body.Close()
	if subResp.StatusCode != http.StatusOK {
		return nil, decodeErr(subResp)
	}
	var out CreateTaskResp
	if err := json.NewDecoder(subResp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("提交响应解析失败: %v", err)
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
// 首跳禁止自动跟随；第二跳不携带云端任务服务凭据。
// onProgress 非空时回报下载字节进度(服务端未给 Content-Length 时 total 为 0)。
func (c *Client) Download(ctx context.Context, taskID, dstPath string, onProgress ProgressFn) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/tasks/"+taskID+"/download", nil)
	if err != nil {
		return err
	}
	firstClient := *c.long
	firstClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := firstClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusFound {
		defer resp.Body.Close()
		return decodeErr(resp)
	}
	location := resp.Header.Get("Location")
	resp.Body.Close()
	if location == "" {
		return fmt.Errorf("下载响应缺 Location")
	}
	resultURL, err := req.URL.Parse(location)
	if err != nil {
		return fmt.Errorf("下载地址解析失败: %v", err)
	}
	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL.String(), nil)
	if err != nil {
		return err
	}
	resp, err = c.long.Do(downloadReq)
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
	total := resp.ContentLength
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

type objectStoreErrBody struct {
	Code      string `json:"Code" xml:"Code"`
	Message   string `json:"Message" xml:"Message"`
	RequestID string `json:"RequestId" xml:"RequestId"`
}

func decodeObjectStoreErr(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body objectStoreErrBody
	if json.Unmarshal(raw, &body) != nil {
		xml.Unmarshal(raw, &body)
	}
	if body.RequestID == "" {
		body.RequestID = resp.Header.Get("X-Tos-Request-Id")
	}
	if body.Message == "" && body.Code == "" {
		body.Message = strings.TrimSpace(string(raw))
	}
	return &ObjectStoreError{
		Status:    resp.StatusCode,
		Code:      body.Code,
		Msg:       body.Message,
		RequestID: body.RequestID,
	}
}

// errBody 服务端错误体(容错解析:code/error/message 都可选)。
type errBody struct {
	Code    string `json:"code"`
	Error   string `json:"error"`
	Message string `json:"message"`
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
func decodeErr(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var b errBody
	json.Unmarshal(raw, &b)
	msg := b.msg()
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuthInvalid
	case http.StatusConflict:
		return ErrTaskNotReady
	}
	return &APIError{Status: resp.StatusCode, Code: b.Code, Msg: msg}
}
