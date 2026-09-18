package main

import (
	"context"

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
		Lab:  d.lab,
		Repo: envOr("DESUB_REPO", `W:\github.com\aura-bootstrap\fengshen_desubber`),
		Bin:  envOr("DESUB_BIN", "/src/bin/desub-lx-v6"),
		GPUs: envOr("DESUB_GPUS", "all"),
	}
	ch := make(chan runner.Event, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			events <- Event{
				Type: ev.Type, Stage: ev.Stage, Done: ev.Done,
				Total: ev.Total, Msg: ev.Msg, Status: ev.Status,
			}
		}
	}()
	err := runner.Run(ctx, o, tk.WorkDir, tk.SrcPath, tk.OutName, tk.ParamsJSON, ch)
	close(ch)
	<-done
	return err
}
