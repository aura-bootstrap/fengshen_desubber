// Package store persists desub tasks in a SQLite database next to the
// engine executable. One row per task: identity, parameter snapshot,
// run state and the final pipeline report.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Task lifecycle: pending -> queued -> running -> succeeded|failed|stopped.
const (
	TaskPending   = "pending"
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
	TaskStopped   = "stopped"
)

type Task struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	SrcPath    string    `json:"src_path"` // host path of the chosen video
	OutName    string    `json:"out_name"`
	ParamsJSON string    `json:"params_json"` // desub flag snapshot
	Status     string    `json:"status"`
	Stage      string    `json:"stage"` // probe|cuts|detect|repair|engine|verify
	Done       int       `json:"done"`  // repaired frames
	Total      int       `json:"total"` // total frames
	WorkDir    string    `json:"work_dir"`
	ReportJSON string    `json:"report_json,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TaskDB struct {
	db *sql.DB
}

func OpenTaskDB(path string) (*TaskDB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Docker Desktop bind mounts corrupt WAL; delete-journal is the safe mode.
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		src_path TEXT NOT NULL,
		out_name TEXT NOT NULL,
		params_json TEXT NOT NULL,
		status TEXT NOT NULL,
		stage TEXT NOT NULL DEFAULT '',
		done INTEGER NOT NULL DEFAULT 0,
		total INTEGER NOT NULL DEFAULT 0,
		work_dir TEXT NOT NULL DEFAULT '',
		report_json TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return &TaskDB{db: db}, nil
}

func (t *TaskDB) Close() error { return t.db.Close() }

const taskCols = `id, name, src_path, out_name, params_json, status, stage, done, total, work_dir, report_json, error, created_at, updated_at`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var tk Task
	var created, updated string
	err := row.Scan(&tk.ID, &tk.Name, &tk.SrcPath, &tk.OutName, &tk.ParamsJSON,
		&tk.Status, &tk.Stage, &tk.Done, &tk.Total, &tk.WorkDir,
		&tk.ReportJSON, &tk.Error, &created, &updated)
	if err != nil {
		return nil, err
	}
	tk.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	tk.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &tk, nil
}

func now() string { return time.Now().Format(time.RFC3339Nano) }

func (t *TaskDB) CreateTask(tk *Task) (int64, error) {
	res, err := t.db.Exec(`INSERT INTO tasks
		(name, src_path, out_name, params_json, status, work_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tk.Name, tk.SrcPath, tk.OutName, tk.ParamsJSON, TaskPending, tk.WorkDir, now(), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (t *TaskDB) GetTask(id int64) (*Task, error) {
	return scanTask(t.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=?`, id))
}

func (t *TaskDB) UpdateWorkDir(id int64, dir string) error {
	_, err := t.db.Exec(`UPDATE tasks SET work_dir=?, updated_at=? WHERE id=?`, dir, now(), id)
	return err
}

func (t *TaskDB) SetStatus(id int64, status string) error {
	_, err := t.db.Exec(`UPDATE tasks SET status=?, updated_at=? WHERE id=?`, status, now(), id)
	return err
}

// UpdateProgress records the current stage and repair frame counters.
func (t *TaskDB) UpdateProgress(id int64, stage string, done, total int) error {
	_, err := t.db.Exec(`UPDATE tasks SET stage=?, done=?, total=?, updated_at=? WHERE id=?`,
		stage, done, total, now(), id)
	return err
}

// Finish sets the terminal status plus the final report or error.
func (t *TaskDB) Finish(id int64, status, report, errMsg string) error {
	_, err := t.db.Exec(`UPDATE tasks SET status=?, report_json=?, error=?, updated_at=? WHERE id=?`,
		status, report, errMsg, now(), id)
	return err
}

func (t *TaskDB) DeleteTask(id int64) error {
	_, err := t.db.Exec(`DELETE FROM tasks WHERE id=?`, id)
	return err
}

// ListTasksBefore returns newest-first tasks with id < beforeID (0 = latest).
func (t *TaskDB) ListTasksBefore(beforeID int64, limit int) ([]Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks`
	args := []any{}
	if beforeID > 0 {
		q += ` WHERE id < ?`
		args = append(args, beforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := t.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tk)
	}
	return out, rows.Err()
}

func (t *TaskDB) CountTasks() (int, error) {
	var n int
	err := t.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n)
	return n, err
}

// LatestTask returns the most recently updated task, or nil.
func (t *TaskDB) LatestTask() (*Task, error) {
	rows, err := t.db.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY updated_at DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanTask(rows)
}

// NextQueued pops the oldest queued task, or nil.
func (t *TaskDB) NextQueued() (*Task, error) {
	rows, err := t.db.Query(`SELECT ` + taskCols + ` FROM tasks WHERE status=? ORDER BY id LIMIT 1`, TaskQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	return scanTask(rows)
}

// ResetStale marks interrupted running/queued tasks as stopped at startup.
func (t *TaskDB) ResetStale() (int64, error) {
	res, err := t.db.Exec(`UPDATE tasks SET status=?, updated_at=? WHERE status IN (?, ?)`,
		TaskStopped, now(), TaskRunning, TaskQueued)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		fmt.Printf("已重置 %d 个中断/排队任务为已停止\n", n)
	}
	return n, nil
}
