package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fengshen-desub/internal/runner"
	"fengshen-desub/internal/store"
)

// dockerRunner adapts runner.Run to the server runner interface.
type dockerRunner struct {
	exeDir string
	lab    string
}

func (d *dockerRunner) Run(ctx context.Context, tk *store.Task, events chan<- Event) error {
	o := runner.DockerOptions{
		Lab:    d.lab,
		Repo:   envOr("DESUB_REPO", `W:\github.com\aura-bootstrap\fengshen_desubber`),
		Bin:    envOr("DESUB_BIN", "/src/bin/desub-lx-v15"),
		GPUs:   envOr("DESUB_GPUS", "all"),
		KeyDir: d.exeDir, // cardkey.json 所在目录,在线模式读卡密用
	}
	ch := make(chan runner.Event, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			events <- Event{
				Type: ev.Type, Stage: ev.Stage, Done: ev.Done,
				Total: ev.Total, Msg: ev.Msg, Status: ev.Status, Balance: ev.Balance,
			}
		}
	}()
	err := runner.Run(ctx, o, tk.WorkDir, tk.SrcPath, tk.OutName, tk.ParamsJSON, ch)
	close(ch)
	<-done
	if err != nil {
		return err
	}
	return copyArtifact(tk.WorkDir, tk.OutName, tk.ParamsJSON)
}

// copyArtifact 任务成功后把产物复制到用户选的输出目录(params.out_dir,
// 空则只留工作区)。复制失败报错但产物仍在工作区,错误信息带原路径。
func copyArtifact(workDir, outName, paramsJSON string) error {
	var p runner.Params
	if err := json.Unmarshal([]byte(paramsJSON), &p); err != nil {
		return nil // 快照损坏已由 runner 报过,这里不重复
	}
	dir := strings.TrimSpace(p.OutDir)
	if dir == "" {
		return nil
	}
	src := filepath.Join(workDir, outName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("产物已生成(%s),但输出目录创建失败: %v", src, err)
	}
	dst := filepath.Join(dir, outName)
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("产物已生成(%s),但读取失败: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("产物已生成(%s),但写入输出目录失败: %v", src, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("产物已生成(%s),但复制到输出目录失败: %v", src, err)
	}
	return out.Close()
}
