package las

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL         string
	apiKey          string
	operatorID      string
	operatorVersion string
	hc              *http.Client
}

func New(baseURL, apiKey, operatorID, operatorVersion string) *Client {
	return &Client{
		baseURL:         baseURL,
		apiKey:          apiKey,
		operatorID:      operatorID,
		operatorVersion: operatorVersion,
		// 单次调用必须在 Lambda 函数预算内返回;跨境链路劣化时挂死整函数(30s timeout)。
		hc: &http.Client{Timeout: 20 * time.Second},
	}
}

type submitResp struct {
	Metadata struct {
		TaskID       string `json:"task_id"`
		TaskStatus   string `json:"task_status"`
		BusinessCode string `json:"business_code"`
		ErrorMsg     string `json:"error_msg"`
	} `json:"metadata"`
}

type pollResp struct {
	Metadata struct {
		TaskStatus   string `json:"task_status"`
		BusinessCode string `json:"business_code"`
		ErrorMsg     string `json:"error_msg"`
	} `json:"metadata"`
	TaskStatus string `json:"task_status"`
	Data       struct {
		VideoURL string  `json:"video_url"`
		Duration float64 `json:"duration"`
	} `json:"data"`
}

func (c *Client) doJSON(ctx context.Context, path string, payload any, out any) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("las %s http %d: %s", path, resp.StatusCode, string(b))
	}
	return json.Unmarshal(b, out)
}

// Submit 提交去字幕任务，返回 LAS task_id。clientToken 用于幂等。
func (c *Client) Submit(ctx context.Context, videoURL, clientToken string) (string, error) {
	var out submitResp
	err := c.doJSON(ctx, "/api/v1/submit", map[string]any{
		"operator_id":      c.operatorID,
		"operator_version": c.operatorVersion,
		"data": map[string]string{
			"video_url":    videoURL,
			"client_token": clientToken,
		},
	}, &out)
	if err != nil {
		return "", err
	}
	if out.Metadata.TaskID == "" {
		return "", fmt.Errorf("las submit failed: %s %s", out.Metadata.BusinessCode, out.Metadata.ErrorMsg)
	}
	return out.Metadata.TaskID, nil
}

// Poll 返回状态（COMPLETED/FAILED/RUNNING/...）、结果 URL、错误信息与权威视频时长。
func (c *Client) Poll(ctx context.Context, taskID string) (status, videoURL, errMsg string, duration float64, err error) {
	var out pollResp
	err = c.doJSON(ctx, "/api/v1/poll", map[string]any{
		"operator_id":      c.operatorID,
		"operator_version": c.operatorVersion,
		"task_id":          taskID,
	}, &out)
	if err != nil {
		return "", "", "", 0, err
	}
	status = out.Metadata.TaskStatus
	if status == "" {
		status = out.TaskStatus
	}
	return status, out.Data.VideoURL, out.Metadata.ErrorMsg, out.Data.Duration, nil
}
