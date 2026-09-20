// desub-engine is the headless task service behind the desub user_client
// Flutter shell: it owns the SQLite task store, schedules containerised
// desub runs one at a time, and streams progress over SSE.
//
// Handshake contract with the frontend (same as fengshen-slicer): started
// with `-serve -port 0`, the first stdout line is `PORT=<n>`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"fengshen-desub/internal/store"
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

// taskRunner executes one task to completion (docker runner in docker.go).
type taskRunner interface {
	Run(ctx context.Context, tk *store.Task, events chan<- Event) error
}

// taskRunState isolates the cancel func of one in-flight task.
type taskRunState struct {
	cancel context.CancelFunc
}

type server struct {
	exeDir     string
	taskDB     *store.TaskDB
	taskMode   bool
	hub        *eventHub
	runner     taskRunner
	mu         sync.Mutex
	runs       map[int64]*taskRunState
	dispatchMu sync.Mutex
}

var errEngineBusy = fmt.Errorf("任务并发槽已满")

func main() {
	serve := flag.Bool("serve", false, "run the HTTP service")
	port := flag.Int("port", 0, "listen port (0 = random)")
	flag.Parse()
	if !*serve {
		fmt.Println("desub-engine: pass -serve to run the task service")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "executable path:", err)
		os.Exit(1)
	}
	s := &server{exeDir: filepath.Dir(exe), hub: newEventHub(), runs: map[int64]*taskRunState{}}
	s.initTaskDB()
	s.runner = &dockerRunner{exeDir: s.exeDir, lab: envOr("DESUB_LAB", `W:\QoderCN\desub-lab`)}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tasks", s.handleTaskCreate)
	mux.HandleFunc("GET /api/tasks", s.handleTasksList)
	mux.HandleFunc("GET /api/tasks/latest", s.handleTaskLatest)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleTaskDetail)
	mux.HandleFunc("POST /api/tasks/{id}/run", s.handleTaskRun)
	mux.HandleFunc("POST /api/tasks/{id}/stop", s.handleTaskStop)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.handleTaskDelete)
	mux.HandleFunc("GET /api/events", s.hub.handleSSE)
	mux.HandleFunc("GET /api/cardkey/status", s.handleCardkeyStatus)
	mux.HandleFunc("POST /api/cardkey/activate", s.handleCardkeyActivate)
	mux.HandleFunc("POST /api/cardkey/deactivate", s.handleCardkeyDeactivate)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_mode": s.taskMode})
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	fmt.Printf("PORT=%d\n", ln.Addr().(*net.TCPAddr).Port)
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

// dockerAvailable is a cheap liveness probe used by the runner.
func dockerAvailable() bool {
	return exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run() == nil
}
