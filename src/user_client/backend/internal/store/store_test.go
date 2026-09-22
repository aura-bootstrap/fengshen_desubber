package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTemp(t *testing.T) *TaskDB {
	t.Helper()
	db, err := OpenTaskDB(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTaskLifecycle(t *testing.T) {
	db := openTemp(t)
	id, err := db.CreateTask(&Task{Name: "ep1", SrcPath: `W:\v\ep1.mp4`, OutName: "ep1_fixed.mp4", ParamsJSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := db.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != TaskPending || tk.Name != "ep1" {
		t.Fatalf("got %+v", tk)
	}
	if err := db.SetStatus(id, TaskRunning); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateProgress(id, "repair", 42, 103); err != nil {
		t.Fatal(err)
	}
	tk, _ = db.GetTask(id)
	if tk.Stage != "repair" || tk.Done != 42 || tk.Total != 103 {
		t.Fatalf("progress not stored: %+v", tk)
	}
	if err := db.Finish(id, TaskSucceeded, `{"elapsed_sec":1}`, ""); err != nil {
		t.Fatal(err)
	}
	tk, _ = db.GetTask(id)
	if tk.Status != TaskSucceeded || tk.ReportJSON == "" {
		t.Fatalf("finish not stored: %+v", tk)
	}
	if err := db.DeleteTask(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetTask(id); err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestQueueAndReset(t *testing.T) {
	db := openTemp(t)
	for _, name := range []string{"a", "b"} {
		id, _ := db.CreateTask(&Task{Name: name, SrcPath: "x", OutName: "y", ParamsJSON: "{}"})
		db.SetStatus(id, TaskQueued)
	}
	tk, err := db.NextQueued()
	if err != nil || tk == nil || tk.Name != "a" {
		t.Fatalf("NextQueued: %v %+v", err, tk)
	}
	if n, _ := db.ResetStale(); n != 2 {
		t.Fatalf("ResetStale reset %d, want 2", n)
	}
	tk, _ = db.NextQueued()
	if tk != nil {
		t.Fatalf("queue should be empty after reset, got %+v", tk)
	}
}

func TestListWindow(t *testing.T) {
	db := openTemp(t)
	for i := 0; i < 5; i++ {
		db.CreateTask(&Task{Name: "t", SrcPath: "x", OutName: "y", ParamsJSON: "{}"})
	}
	list, err := db.ListTasksBefore(0, 3)
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	if list[0].ID != 5 || list[2].ID != 3 {
		t.Fatalf("order wrong: %+v", list)
	}
	older, _ := db.ListTasksBefore(3, 10)
	if len(older) != 2 || older[0].ID != 2 {
		t.Fatalf("window wrong: %+v", older)
	}
	if n, _ := db.CountTasks(); n != 5 {
		t.Fatalf("count %d, want 5", n)
	}
}

// 外部连接持写锁时,busy_timeout 让写入等待锁释放而不是立刻 SQLITE_BUSY。
func TestBusyTimeoutWaitsForExternalLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	db, err := OpenTaskDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id, err := db.CreateTask(&Task{Name: "x", SrcPath: "s", OutName: "o", ParamsJSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}

	// 模拟另一个进程(如残留引擎实例)持写锁 300ms。
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	tx, err := locker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE tasks SET name=name WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		tx.Rollback()
	}()

	start := time.Now()
	if err := db.SetStatus(id, TaskRunning); err != nil {
		t.Fatalf("write under external lock should wait and succeed, got %v", err)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Fatalf("returned in %v without waiting for lock release", el)
	}
}

// 单连接串行化:多 goroutine 读写互撞不应出现 SQLITE_BUSY。
func TestConcurrentReadWrite(t *testing.T) {
	db := openTemp(t)
	id, err := db.CreateTask(&Task{Name: "x", SrcPath: "s", OutName: "o", ParamsJSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if i%2 == 0 {
					if err := db.UpdateProgress(id, "repair", j, 20); err != nil {
						errs <- err
					}
				} else if _, err := db.ListTasksBefore(0, 5); err != nil {
					errs <- err
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
