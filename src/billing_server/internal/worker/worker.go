package worker

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
)

type Worker struct {
	st        *ddbstore.Store
	reg       *provider.Registry
	resultDir string
	PollEvery time.Duration
	PollMax   time.Duration
}

func New(st *ddbstore.Store, reg *provider.Registry, resultDir string) *Worker {
	return &Worker{
		st: st, reg: reg, resultDir: resultDir,
		PollEvery: 15 * time.Second, PollMax: 30 * time.Minute,
	}
}

// Run 串行处理队列，直到 ctx 取消。
func (w *Worker) Run(ctx context.Context) {
	if err := w.st.ResetProcessing(ctx); err != nil {
		log.Printf("reset processing: %v", err)
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		t, err := w.st.NextQueued(ctx)
		if err != nil {
			log.Printf("next queued: %v", err)
			continue
		}
		if t == nil {
			continue
		}
		w.process(ctx, t)
	}
}

func (w *Worker) fail(ctx context.Context, t *ddbstore.Task, err error) {
	log.Printf("task %s failed: %v", t.ID, err)
	if ferr := w.st.FailTaskWithRefund(ctx, t.ID, err.Error()); ferr != nil {
		log.Printf("task %s refund failed: %v", t.ID, ferr)
	}
}

func (w *Worker) process(ctx context.Context, t *ddbstore.Task) {
	log.Printf("task %s: processing (%ds, cost %d)", t.ID, t.DurationSec, t.Cost)

	// 按任务落库的平台名取能力；空 = 默认平台（创建时已校验，未知名按失败处理）。
	p := w.reg.Default()
	if t.Provider != "" {
		var ok bool
		p, ok = w.reg.Get(t.Provider)
		if !ok {
			w.fail(ctx, t, fmt.Errorf("unknown provider: %s", t.Provider))
			return
		}
	}

	key := fmt.Sprintf("input/%s%s", t.ID, filepath.Ext(t.SrcPath))
	url, err := retry(ctx, 3, func() (string, error) {
		return p.Uploader.UploadAndPresign(ctx, t.SrcPath, key, 7200)
	})
	if err != nil {
		w.fail(ctx, t, fmt.Errorf("tos upload: %w", err))
		return
	}

	lasID, err := retry(ctx, 3, func() (string, error) {
		return p.Operator.Submit(ctx, url, t.ID)
	})
	if err != nil {
		w.fail(ctx, t, fmt.Errorf("las submit: %w", err))
		return
	}
	if err := w.st.SetLasTaskID(ctx, t.ID, lasID); err != nil {
		log.Printf("task %s set las id: %v", t.ID, err)
	}

	deadline := time.Now().Add(w.PollMax)
	for {
		status, videoURL, errMsg, err := p.Operator.Poll(ctx, lasID)
		if err != nil {
			log.Printf("task %s poll error: %v", t.ID, err)
		} else {
			switch status {
			case "COMPLETED":
				dst := filepath.Join(w.resultDir, t.ID+".mp4")
				if _, err := retry(ctx, 3, func() (string, error) {
					return "", p.Operator.Download(ctx, videoURL, dst)
				}); err != nil {
					w.fail(ctx, t, fmt.Errorf("result download: %w", err))
					return
				}
				if err := w.st.CompleteTask(ctx, t.ID, dst); err != nil {
					log.Printf("task %s complete: %v", t.ID, err)
				}
				log.Printf("task %s: completed", t.ID)
				return
			case "FAILED":
				w.fail(ctx, t, fmt.Errorf("las failed: %s", errMsg))
				return
			}
		}
		if time.Now().After(deadline) {
			w.fail(ctx, t, fmt.Errorf("poll timeout after %s", w.PollMax))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.PollEvery):
		}
	}
}

func retry(ctx context.Context, n int, fn func() (string, error)) (string, error) {
	var err error
	for i := 0; i < n; i++ {
		var v string
		v, err = fn()
		if err == nil {
			return v, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(1<<i) * 2 * time.Second):
		}
	}
	return "", err
}
