package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	testToken = "dev-session.token"
	testHash  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// newFakeServer 按云端内部任务契约起 httptest 假服务,返回 server 与请求记录。
// /v1/admin/login、/tos/ 直传与 /result.mp4 第二跳免验统一任务头。
func newFakeServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/admin/login" && !strings.HasPrefix(r.URL.Path, "/tos/") && r.URL.Path != "/result.mp4" {
			if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
				t.Errorf("Authorization 头错误: %q", got)
			}
			if got := r.Header.Get("X-Machine-Hash"); got != testHash {
				t.Errorf("X-Machine-Hash 头错误: %q", got)
			}
		}
		seen = append(seen, r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func newTestClient(srv *httptest.Server) *Client {
	return New(srv.URL+"/", testToken, testHash)
}

func TestLoginOK(t *testing.T) {
	srv, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/admin/login" || r.Method != http.MethodPost {
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("登录请求解析失败: %v", err)
		}
		if body.Username != "root" || body.Password != "secret" {
			t.Errorf("登录请求不符: %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"token": testToken, "username": "root", "role": "root",
		})
	})
	resp, err := Login(context.Background(), srv.URL+"/", "root", "secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Token != testToken || resp.Username != "root" || resp.Role != "root" {
		t.Fatalf("登录响应不符: %+v", resp)
	}
}

func TestLoginErrorMapping(t *testing.T) {
	srv, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"用户名或密码错误"}`)
	})
	_, err := Login(context.Background(), srv.URL, "root", "bad")
	if !errors.Is(err, ErrAuthInvalid) {
		t.Fatalf("err = %v, want ErrAuthInvalid", err)
	}
}

func TestCreateTaskStreamsUpload(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "样片 01.mp4")
	payload := strings.Repeat("fake-mp4-bytes;", 4096)
	if err := os.WriteFile(video, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	srv2, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/tasks" && r.Method == http.MethodPost:
			if got := r.Header.Get("X-Video-Filename"); got != "样片 01.mp4" {
				t.Errorf("X-Video-Filename = %q", got)
			}
			if got := r.Header.Get("X-Provider"); got != "diffueraser" {
				t.Errorf("X-Provider = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "task-1", "upload_url": srv.URL + "/tos/input/task-1.mp4", "expires_in": 7200,
				"account": nil,
			})
		case r.URL.Path == "/tos/input/task-1.mp4" && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if string(body) != payload {
				t.Errorf("直传体不一致(收到 %d 字节)", len(body))
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/v1/tasks/task-1/submit" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "task-1", "duration_sec": 12.5, "cost": 0, "account": nil,
			})
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
		}
	})
	srv = srv2
	var lastDone, totalSeen int64 = -1, -1
	resp, err := newTestClient(srv).CreateTask(context.Background(), video, "diffueraser",
		func(done, total int64) { lastDone, totalSeen = done, total })
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if resp.TaskID != "task-1" || resp.Cost != 0 || resp.DurationSec != 12.5 {
		t.Fatalf("建单响应不符: %+v", resp)
	}
	if lastDone != int64(len(payload)) || totalSeen != int64(len(payload)) {
		t.Fatalf("上传进度回调不符: done=%d total=%d, want %d", lastDone, totalSeen, len(payload))
	}
}

func TestCreateTaskTOSUploadError(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"JSON", "application/json", `{"Code":"InvalidAccessKeyId","Message":"access key does not exist","RequestId":"req-json"}`},
		{"XML", "application/xml", `<Error><Code>AccessDenied</Code><Message>put denied</Message><RequestId>req-xml</RequestId></Error>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			video := filepath.Join(dir, "a.mp4")
			if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			var srv *httptest.Server
			submitCalled := false
			srv2, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/tasks":
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]string{"task_id": "task-1", "upload_url": srv.URL + "/tos/input/task-1.mp4"})
				case r.URL.Path == "/tos/input/task-1.mp4":
					w.Header().Set("Content-Type", tc.contentType)
					w.Header().Set("X-Tos-Request-Id", "req-header")
					w.WriteHeader(http.StatusForbidden)
					io.WriteString(w, tc.body)
				case r.URL.Path == "/v1/tasks/task-1/submit":
					submitCalled = true
				default:
					t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
				}
			})
			srv = srv2

			_, err := newTestClient(srv).CreateTask(context.Background(), video, "", nil)
			var storeErr *ObjectStoreError
			if !errors.As(err, &storeErr) {
				t.Fatalf("err = %T %v, want *ObjectStoreError", err, err)
			}
			if storeErr.Code == "" || storeErr.RequestID == "" {
				t.Fatalf("对象存储错误信息不完整: %+v", storeErr)
			}
			if submitCalled {
				t.Fatal("上传失败后不应调用 submit")
			}
			message := Message(err)
			if strings.Contains(message, "云端服务不可达") || !strings.Contains(message, storeErr.Code) {
				t.Fatalf("错误文案分类不正确: %q", message)
			}
		})
	}
}

func TestGetTaskStatus(t *testing.T) {
	var status string
	srv, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks/task-9" {
			t.Errorf("意外路径: %s", r.URL.Path)
		}
		out := map[string]any{
			"task_id": "task-9", "status": status, "duration_sec": 3, "cost": 0,
			"account": nil,
		}
		if status == "failed" {
			out["error"] = "provider 内部错误"
		}
		json.NewEncoder(w).Encode(out)
	})
	cli := newTestClient(srv)
	status = "processing"
	info, err := cli.GetTask(context.Background(), "task-9")
	if err != nil || info.Status != "processing" || info.Cost != 0 {
		t.Fatalf("GetTask = %+v, %v", info, err)
	}
	status = "failed"
	info, err = cli.GetTask(context.Background(), "task-9")
	if err != nil {
		t.Fatalf("GetTask failed 状态不应返回传输错误: %v", err)
	}
	if info.Status != "failed" || info.Error != "provider 内部错误" {
		t.Fatalf("失败任务信息不符: %+v", info)
	}
}

func TestDownloadStreamsToDisk(t *testing.T) {
	payload := strings.Repeat("mp4-result;", 8192)
	srv, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/tasks/task-1/download":
			http.Redirect(w, r, "/result.mp4", http.StatusFound)
		case "/result.mp4":
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-Machine-Hash") != "" {
				t.Errorf("成片第二跳不应携带云端服务凭据")
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			io.WriteString(w, payload)
		default:
			t.Errorf("意外路径: %s", r.URL.Path)
		}
	})
	dst := filepath.Join(t.TempDir(), "out.mp4")
	var lastDone, totalSeen int64 = -1, -1
	err := newTestClient(srv).Download(context.Background(), "task-1", dst,
		func(done, total int64) { lastDone, totalSeen = done, total })
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if lastDone != int64(len(payload)) || totalSeen != int64(len(payload)) {
		t.Fatalf("下载进度回调不符: done=%d total=%d, want %d", lastDone, totalSeen, len(payload))
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != payload {
		t.Fatalf("下载内容不符(err=%v, %d 字节)", err, len(got))
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatalf(".part 临时文件应已被 rename")
	}
}

func TestDownloadNotReady(t *testing.T) {
	srv, _ := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "task not completed"})
	})
	dst := filepath.Join(t.TempDir(), "out.mp4")
	err := newTestClient(srv).Download(context.Background(), "task-1", dst, nil)
	if !errors.Is(err, ErrTaskNotReady) {
		t.Fatalf("err = %v, want ErrTaskNotReady", err)
	}
}

func TestMessageMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{ErrAuthInvalid, "认证失败"},
		{ErrTaskNotReady, "尚未完成"},
	} {
		if msg := Message(tc.err); !strings.Contains(msg, tc.want) {
			t.Fatalf("Message(%v) = %q, 应包含 %q", tc.err, msg, tc.want)
		}
	}
}
