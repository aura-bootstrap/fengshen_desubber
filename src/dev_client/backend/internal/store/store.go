// Package store persists desub dev-engine tasks and config history in a
// SQLite database next to the engine executable. Adapted from the
// user_client task store; adds the config_history table (same contract as
// fengshen-slicer's dev client).
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
	SrcPath    string    `json:"src_path"`
	OutName    string    `json:"out_name"`
	ParamsJSON string    `json:"params_json"` // config snapshot at creation
	Status     string    `json:"status"`
	Stage      string    `json:"stage"` // probe|cuts|detect|ocr|engine|repair|verify
	Done       int       `json:"done"`
	Total      int       `json:"total"`
	WorkDir    string    `json:"work_dir"`
	ReportJSON string    `json:"report_json,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ConfigHistEntry is one config-history index row (sidebar version list).
type ConfigHistEntry struct {
	Version int64 `json:"version"`
	SavedAt int64 `json:"saved_at"`
}

const configHistKeep = 50

type TaskDB struct {
	db *sql.DB
}

func OpenTaskDB(path string) (*TaskDB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// 单连接串行化:DELETE 日志模式下读写互斥,连接池多连接撞车会直接
	// SQLITE_BUSY(默认 busy_timeout=0);限 1 连接后 busy_timeout 常驻生效,
	// 跨进程占用(如残留引擎实例)最多等 5s 而不是立刻报错。
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
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
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS config_history(
		version INTEGER PRIMARY KEY AUTOINCREMENT,
		saved_at INTEGER NOT NULL,
		payload TEXT NOT NULL
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

// UpdateParams 覆盖任务的参数快照(重跑前换引擎等场景)。
func (t *TaskDB) UpdateParams(id int64, params string) error {
	_, err := t.db.Exec(`UPDATE tasks SET params_json=?, updated_at=? WHERE id=?`, params, now(), id)
	return err
}

func (t *TaskDB) SetStatus(id int64, status string) error {
	_, err := t.db.Exec(`UPDATE tasks SET status=?, updated_at=? WHERE id=?`, status, now(), id)
	return err
}

func (t *TaskDB) UpdateProgress(id int64, stage string, done, total int) error {
	_, err := t.db.Exec(`UPDATE tasks SET stage=?, done=?, total=?, updated_at=? WHERE id=?`,
		stage, done, total, now(), id)
	return err
}

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
	out := []Task{}
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

func (t *TaskDB) NextQueued() (*Task, error) {
	rows, err := t.db.Query(`SELECT `+taskCols+` FROM tasks WHERE status=? ORDER BY id LIMIT 1`, TaskQueued)
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

// AddConfigHistory records one config snapshot (version auto-increments),
// trimming to the most recent configHistKeep entries.
func (t *TaskDB) AddConfigHistory(payload string) (int64, error) {
	res, err := t.db.Exec(`INSERT INTO config_history(saved_at,payload) VALUES(?,?)`, time.Now().Unix(), payload)
	if err != nil {
		return 0, err
	}
	v, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := t.db.Exec(`DELETE FROM config_history WHERE version <= ?`, v-configHistKeep); err != nil {
		return v, err
	}
	return v, nil
}

// ListConfigHistory returns version index entries, newest first.
func (t *TaskDB) ListConfigHistory() ([]ConfigHistEntry, error) {
	rows, err := t.db.Query(`SELECT version, saved_at FROM config_history ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConfigHistEntry{}
	for rows.Next() {
		var e ConfigHistEntry
		if err := rows.Scan(&e.Version, &e.SavedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetConfigHistory returns one history payload; ok=false when absent.
func (t *TaskDB) GetConfigHistory(version int64) (string, bool, error) {
	var payload string
	err := t.db.QueryRow(`SELECT payload FROM config_history WHERE version=?`, version).Scan(&payload)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return payload, true, nil
}
