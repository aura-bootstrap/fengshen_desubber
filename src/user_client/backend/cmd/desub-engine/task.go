package main

// 任务体系:创建/运行/查询/删除 + 状态机。
// 单 GPU 槽:同一时刻只跑一个容器,忙时入队(status=queued),结束自动调度下一个;
// 手动停止出队为 stopped。运行即重跑(desub 无断点续跑)。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fengshen-desub/internal/cardkey"
	"fengshen-desub/internal/runner"
	"fengshen-desub/internal/store"
)

func (s *server) taskRoot() string { return filepath.Join(s.exeDir, "workspace", "tasks") }

// handleTaskCreate POST /api/tasks {name,src_path,out_name,params,run_now}
// params 是 desub 旗标快照(propainter/grain/ocr/crf/force_engine...),随任务固化。
func (s *server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeErr(w, http.StatusServiceUnavailable, "任务功能不可用(任务库打开失败)")
		return
	}
	var req struct {
		Name    string         `json:"name"`
		SrcPath string         `json:"src_path"`
		OutName string         `json:"out_name"`
		Params  map[string]any `json:"params"`
		RunNow  bool           `json:"run_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求解析失败")
		return
	}
	src := strings.TrimSpace(req.SrcPath)
	if src == "" {
		writeErr(w, http.StatusUnprocessableEntity, "未选择视频文件")
		return
	}
	if _, err := os.Stat(src); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "视频文件不存在: "+src)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	if strings.TrimSpace(req.OutName) == "" {
		req.OutName = req.Name + "_fixed.mp4"
	}
	snap, _ := json.Marshal(req.Params)

	// 在线去字幕前置校验:未激活卡密直接在创建期 400,不入库(尽早失败)。
	var rp runner.Params
	if err := json.Unmarshal(snap, &rp); err == nil && rp.Online {
		if _, err := cardkey.LoadKeyFile(s.exeDir); err != nil {
			writeErr(w, http.StatusBadRequest, "在线去字幕需要先激活卡密(设置页 -> 卡密激活)")
			return
		}
	}
	// 输出目录创建期预建(尽早失败):选了目录但建不了就直接 422,不入库。
	if d := strings.TrimSpace(rp.OutDir); d != "" {
		if err := os.MkdirAll(d, 0o755); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "输出目录不可用: "+d)
			return
		}
	}

	tk := &store.Task{
		Name: req.Name, SrcPath: src, OutName: req.OutName, ParamsJSON: string(snap),
	}
	os.MkdirAll(s.taskRoot(), 0o755)
	id, err := s.taskDB.CreateTask(tk)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tk.ID = id
	tk.WorkDir = filepath.Join(s.taskRoot(), fmt.Sprint(id))
	if err := os.MkdirAll(tk.WorkDir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, "工作区创建失败: "+err.Error())
		return
	}
	if err := s.taskDB.UpdateWorkDir(id, tk.WorkDir); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fresh, err := s.taskDB.GetTask(id); err == nil {
		tk = fresh
	}

	if req.RunNow {
		if err := s.startTaskRun(tk); err != nil {
			if errors.Is(err, errEngineBusy) {
				s.taskDB.SetStatus(tk.ID, store.TaskQueued)
				tk.Status = store.TaskQueued
				s.hub.publish(Event{Type: "queue"})
			} else {
				writeErr(w, http.StatusConflict, err.Error())
				return
			}
		} else {
			tk.Status = store.TaskRunning
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": tk})
}

// handleTaskRun POST /api/tasks/{id}/run — 引擎忙时入队而非报错。
func (s *server) handleTaskRun(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeErr(w, http.StatusServiceUnavailable, "任务功能不可用")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	tk, err := s.taskDB.GetTask(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	s.mu.Lock()
	_, runningThis := s.runs[id]
	s.mu.Unlock()
	if tk.Status == store.TaskRunning || runningThis {
		writeErr(w, http.StatusConflict, fmt.Sprintf("任务 %d 正在运行", tk.ID))
		return
	}
	if tk.Status == store.TaskQueued {
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": tk.ID, "status": "queued"})
		return
	}
	if err := s.startTaskRun(tk); err != nil {
		if errors.Is(err, errEngineBusy) {
			s.taskDB.SetStatus(tk.ID, store.TaskQueued)
			s.hub.publish(Event{Type: "log", TaskID: tk.ID,
				Msg: fmt.Sprintf("任务 #%d「%s」已加入队列", tk.ID, tk.Name)})
			s.hub.publish(Event{Type: "queue"})
			writeJSON(w, http.StatusAccepted, map[string]any{"task_id": tk.ID, "status": "queued"})
			return
		}
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": tk.ID, "status": "started"})
}

func (s *server) startTaskRun(tk *store.Task) error {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if len(s.runs) >= 1 {
		s.mu.Unlock()
		cancel()
		return errEngineBusy
	}
	s.runs[tk.ID] = &taskRunState{cancel: cancel}
	s.mu.Unlock()
	if err := s.taskDB.SetStatus(tk.ID, store.TaskRunning); err != nil {
		s.mu.Lock()
		delete(s.runs, tk.ID)
		s.mu.Unlock()
		cancel()
		return err
	}

	events := make(chan Event, 256)
	go s.consume(events, tk.ID)
	go func() {
		err := s.runner.Run(ctx, tk, events)
		if err != nil && !errors.Is(err, context.Canceled) {
			events <- Event{Type: "done", Status: store.TaskFailed, Msg: err.Error()}
			return
		}
		if errors.Is(err, context.Canceled) {
			events <- Event{Type: "done", Status: store.TaskStopped}
			return
		}
		events <- Event{Type: "done", Status: store.TaskSucceeded}
	}()
	return nil
}

// consume 把 runner 事件落库并转发 SSE。
func (s *server) consume(events <-chan Event, taskID int64) {
	var report string
	for ev := range events {
		ev.TaskID = taskID
		switch ev.Type {
		case "stage":
			s.taskDB.UpdateProgress(taskID, ev.Stage, 0, 0)
		case "progress":
			// 在线链路上传/下载带 Stage;本地 docker 管线只有 repair 帧计数(空值回落)。
			stage := ev.Stage
			if stage == "" {
				stage = "repair"
			}
			s.taskDB.UpdateProgress(taskID, stage, ev.Done, ev.Total)
		case "report":
			report = ev.Msg
			continue // 不转发原始报告,前端走详情接口取
		case "done":
			if ev.Status == store.TaskFailed {
				s.taskDB.Finish(taskID, store.TaskFailed, report, ev.Msg)
			} else {
				s.taskDB.Finish(taskID, ev.Status, report, "")
			}
			s.mu.Lock()
			if run, ok := s.runs[taskID]; ok && run.cancel != nil {
				run.cancel()
			}
			delete(s.runs, taskID)
			s.mu.Unlock()
			s.hub.publish(ev)
			go s.startNextQueued()
			return
		}
		s.hub.publish(ev)
	}
}

// startNextQueued 任务结束后自动调度下一个排队任务。
func (s *server) startNextQueued() {
	if !s.taskMode {
		return
	}
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	tk, err := s.taskDB.NextQueued()
	if err != nil || tk == nil {
		return
	}
	s.hub.publish(Event{Type: "log", Msg: fmt.Sprintf("队列调度:任务 #%d「%s」开始运行", tk.ID, tk.Name)})
	if err := s.startTaskRun(tk); err != nil {
		if errors.Is(err, errEngineBusy) {
			return
		}
		s.hub.publish(Event{Type: "log", Msg: fmt.Sprintf("任务 #%d 启动失败(跳过): %v", tk.ID, err)})
		s.taskDB.SetStatus(tk.ID, store.TaskFailed)
	}
	s.hub.publish(Event{Type: "queue"})
}

func (s *server) handleTasksList(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeJSON(w, http.StatusOK, map[string]any{"tasks": []any{}, "degraded": true})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	beforeID, _ := strconv.ParseInt(q.Get("before_id"), 10, 64)
	list, err := s.taskDB.ListTasksBefore(beforeID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.taskDB.CountTasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list, "total": total})
}

func (s *server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeErr(w, http.StatusServiceUnavailable, "任务功能不可用")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	tk, err := s.taskDB.GetTask(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, tk)
}

func (s *server) handleTaskLatest(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	tk, err := s.taskDB.LatestTask()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": tk})
}

// handleTaskStop POST /api/tasks/{id}/stop — 排队则出队,运行则取消。
func (s *server) handleTaskStop(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	run, running := s.runs[id]
	s.mu.Unlock()
	if !running && s.taskMode {
		if tk, err := s.taskDB.GetTask(id); err == nil && tk.Status == store.TaskQueued {
			s.taskDB.SetStatus(id, store.TaskStopped)
			s.hub.publish(Event{Type: "log", TaskID: id,
				Msg: fmt.Sprintf("任务 #%d「%s」已暂停(移出队列)", tk.ID, tk.Name)})
			s.hub.publish(Event{Type: "queue"})
			writeJSON(w, http.StatusOK, map[string]string{"status": "dequeued"})
			return
		}
		writeErr(w, http.StatusConflict, "该任务未在运行也未排队")
		return
	}
	if run != nil && run.cancel != nil {
		run.cancel()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (s *server) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if !s.taskMode {
		writeErr(w, http.StatusServiceUnavailable, "任务功能不可用")
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	_, busy := s.runs[id]
	s.mu.Unlock()
	if busy {
		writeErr(w, http.StatusConflict, "任务正在运行,不能删除")
		return
	}
	tk, err := s.taskDB.GetTask(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	if tk.WorkDir != "" {
		if err := os.RemoveAll(tk.WorkDir); err != nil {
			writeErr(w, http.StatusInternalServerError, "工作区删除失败: "+err.Error())
			return
		}
	}
	if err := s.taskDB.DeleteTask(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
