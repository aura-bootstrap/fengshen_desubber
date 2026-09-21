// Command desub-engine is the local HTTP task service behind the desub
// dev_client Flutter shell: config editing with history, a SQLite task
// store, one-at-a-time local `desub` runs, and SSE progress. It never
// talks to the billing server — the dev client is fully local.
//
// Handshake contract with the frontend (same as fengshen-slicer): started
// with `-serve -port 0`, the first stdout line is `PORT=<n>`; the process
// exits when the parent closes stdin (orphan guard).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/aura-bootstrap/fengshen_desubber/internal/store"
)

// Event is one SSE frame pushed to the frontend.
type Event struct {
	Type   string `json:"type"` // progress|stage|log|queue|done
	TaskID int64  `json:"task_id,omitempty"`
	Stage  string `json:"stage,omitempty"`
	Done   int    `json:"done,omitempty"`
	Total  int    `json:"total,omitempty"`
	Msg    string `json:"msg,omitempty"`
	Status string `json:"status,omitempty"`
}

// taskRunState isolates the cancel func of one in-flight task.
type taskRunState struct {
	cancel context.CancelFunc
}

type server struct {
	exeDir     string
	cfgPath    string
	taskDB     *store.TaskDB
	taskMode   bool
	hub        *eventHub
	mu         sync.Mutex
	runs       map[int64]*taskRunState
	dispatchMu sync.Mutex
}

var errEngineBusy = fmt.Errorf("引擎忙:同一时刻只跑一个任务")

func main() {
	serve := flag.Bool("serve", false, "run the HTTP service")
	port := flag.Int("port", 0, "listen port (0 = random)")
	flag.Parse()
	if !*serve {
		fmt.Fprintln(os.Stderr, "desub-engine: pass -serve to run the task service")
		os.Exit(2)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "executable path:", err)
		os.Exit(1)
	}
	exeDir, _ := filepath.Abs(filepath.Dir(exe))
	os.Chdir(exeDir) // anchor relative paths next to the exe (portable semantics)

	unlock, err := acquireLock(filepath.Join(exeDir, "app.lock"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "已有实例在运行")
		os.Exit(1)
	}
	defer unlock()

	s := &server{
		exeDir:  exeDir,
		cfgPath: filepath.Join(exeDir, "config.json"),
		hub:     newEventHub(),
		runs:    map[int64]*taskRunState{},
	}
	s.initTaskDB()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("GET /api/config/history", s.handleConfigHistory)
	mux.HandleFunc("GET /api/config/history/{version}", s.handleConfigHistoryGet)
	mux.HandleFunc("POST /api/tasks", s.handleTaskCreate)
	mux.HandleFunc("GET /api/tasks", s.handleTasksList)
	mux.HandleFunc("GET /api/tasks/latest", s.handleTaskLatest)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTaskDetail)
	mux.HandleFunc("POST /api/tasks/{id}/run", s.handleTaskRun)
	mux.HandleFunc("POST /api/tasks/{id}/stop", s.handleTaskStop)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.handleTaskDelete)
	mux.HandleFunc("GET /api/events", s.hub.handleSSE)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_mode": s.taskMode})
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	fmt.Printf("PORT=%d\n", ln.Addr().(*net.TCPAddr).Port)

	// Orphan guard: the Flutter parent closing stdin means we should exit.
	go func() {
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}()

	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// initTaskDB opens the task store; failure degrades to no-task mode.
func (s *server) initTaskDB() {
	db, err := store.OpenTaskDB(filepath.Join(s.exeDir, "tasks.db"))
	if err != nil {
		fmt.Println("任务库打开失败(降级为无任务模式):", err)
		return
	}
	if _, err := db.ResetStale(); err != nil {
		fmt.Println("重置中断任务失败:", err)
	}
	s.taskDB = db
	s.taskMode = true
}
