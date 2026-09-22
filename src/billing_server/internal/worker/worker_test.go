package worker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fengshen-desubber/billing_server/internal/cardkey"
	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
	"fengshen-desubber/billing_server/internal/testutil"
)

// recUploader 记录 Delete 调用键的假上传器。
type recUploader struct {
	mu      sync.Mutex
	deleted []string
}

func (r *recUploader) UploadAndPresign(ctx context.Context, localPath, key string, expires int64) (string, error) {
	return "https://tos.fake/" + key, nil
}

func (r *recUploader) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, key)
	return nil
}

func (r *recUploader) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.deleted...)
}

type stubOperator struct{ fail bool }

func (s *stubOperator) Submit(ctx context.Context, videoURL, clientToken string) (string, error) {
	return "las-task-1", nil
}

func (s *stubOperator) Poll(ctx context.Context, taskID string) (string, string, string, error) {
	if s.fail {
		return "FAILED", "", "boom", nil
	}
	return "COMPLETED", "https://result.fake/v.mp4", "", nil
}

func (s *stubOperator) Download(ctx context.Context, url, dst string) error {
	return os.WriteFile(dst, []byte("mp4"), 0o644)
}

// newTask 造一张已核销卡 + 一个排队任务(源片为真实临时文件)。
func newTask(t *testing.T, st *ddbstore.Store, dir string) *ddbstore.Task {
	t.Helper()
	ctx := context.Background()
	code, _ := cardkey.Generate()
	c, err := st.CreateCard(ctx, "worker-test", code, 10, "")
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	machine := "mach-worker-" + code[:8]
	if _, err := st.EnsureRedeemed(ctx, c, machine); err != nil {
		t.Fatalf("EnsureRedeemed: %v", err)
	}
	src := filepath.Join(dir, "src.mp4")
	testutil.WriteTestMP4(t, src, 5)
	task := &ddbstore.Task{
		ID: "task-" + code[:8], CardID: c.ID, Provider: "las",
		SrcPath: src, DurationSec: 5, Cost: 1,
	}
	if _, err := st.CreateTaskWithDebit(ctx, task); err != nil {
		t.Fatalf("CreateTaskWithDebit: %v", err)
	}
	return task
}

func TestProcessDeletesTOSOnComplete(t *testing.T) {
	testutil.FreshTable(t)
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	task := newTask(t, st, dir)

	up := &recUploader{}
	reg := provider.NewRegistry("las")
	reg.Register(provider.Provider{Name: "las", Uploader: up, Operator: &stubOperator{}})
	w := New(st, reg, dir)
	w.PollEvery = 10 * time.Millisecond

	queued, err := st.NextQueued(context.Background())
	if err != nil || queued == nil {
		t.Fatalf("NextQueued: %v %+v", err, queued)
	}
	w.process(context.Background(), queued)

	want := "input/" + task.ID + ".mp4"
	if del := up.got(); len(del) != 1 || del[0] != want {
		t.Fatalf("tos delete = %v, want [%s]", del, want)
	}
	done, _ := st.GetTask(context.Background(), task.ID)
	if done.Status != ddbstore.TaskCompleted {
		t.Fatalf("status = %s", done.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, task.ID+".mp4")); err != nil {
		t.Fatalf("result missing: %v", err)
	}
}

func TestProcessDeletesTOSOnFailure(t *testing.T) {
	testutil.FreshTable(t)
	st, err := ddbstore.NewFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	task := newTask(t, st, dir)

	up := &recUploader{}
	reg := provider.NewRegistry("las")
	reg.Register(provider.Provider{Name: "las", Uploader: up, Operator: &stubOperator{fail: true}})
	w := New(st, reg, dir)
	w.PollEvery = 10 * time.Millisecond

	queued, err := st.NextQueued(context.Background())
	if err != nil || queued == nil {
		t.Fatalf("NextQueued: %v %+v", err, queued)
	}
	w.process(context.Background(), queued)

	want := "input/" + task.ID + ".mp4"
	if del := up.got(); len(del) != 1 || del[0] != want {
		t.Fatalf("tos delete = %v, want [%s]", del, want)
	}
	done, _ := st.GetTask(context.Background(), task.ID)
	if done.Status != ddbstore.TaskFailed {
		t.Fatalf("status = %s", done.Status)
	}
}
