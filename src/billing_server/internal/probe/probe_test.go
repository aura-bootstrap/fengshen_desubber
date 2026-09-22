package probe

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/billing"
	"fengshen-desubber/billing_server/internal/testutil"
)

var time0 = time.Unix(0, 0)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func TestDurationSeconds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, 65)
	got, err := DurationSeconds(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != 65 {
		t.Fatalf("want 65, got %d", got)
	}
	if c := billing.Cost(got); c != 2 {
		t.Fatalf("65s should cost 2 credits, got %d", c)
	}
}

func TestDurationSecondsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.mp4")
	testutil.WriteBadFile(t, p)
	if _, err := DurationSeconds(p); err == nil {
		t.Fatal("expect error for non-mp4")
	}
}

// TestDurationSecondsURL 经 HTTP Range 服务器读时长:结果须与本地读取一致。
// http.ServeContent 自带 Range 支持,与 TOS 预签名 GET 行为对齐。
func TestDurationSecondsURL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v.mp4")
	testutil.WriteTestMP4(t, p, 65)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "v.mp4", time0, bytesReader(data))
	}))
	defer srv.Close()
	got, err := DurationSecondsURL(srv.URL + "/v.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if got != 65 {
		t.Fatalf("want 65, got %d", got)
	}
}
