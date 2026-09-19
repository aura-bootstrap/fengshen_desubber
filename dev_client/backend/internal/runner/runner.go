// Package runner executes one desub task inside the ML container: it stages
// the input video into the task work dir, assembles the docker run command
// (same mounts/env as scripts/run_container.sh), and parses desub's stdout
// into stage/progress/report events.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Event mirrors the engine's SSE event; duplicated here to keep the runner
// package free of the server package.
type Event struct {
	Type   string
	Stage  string
	Done   int
	Total  int
	Msg    string
	Status string
}

type DockerOptions struct {
	Image string // desub:cu124
	Lab   string // host path mounted at /work
	Repo  string // host path mounted at /src
	Bin   string // container path of the desub binary
	GPUs  string // "all" enables --gpus; empty disables
	Proxy string // host Clash endpoint
}

func (o *DockerOptions) defaults() {
	if o.Image == "" {
		o.Image = "desub:cu124"
	}
	if o.Bin == "" {
		o.Bin = "/src/bin/desub-lx-v11"
	}
	if o.Proxy == "" {
		o.Proxy = "http://host.docker.internal:7897"
	}
}

// Params is the per-task flag snapshot stored with the task row.
type Params struct {
	Propainter  bool   `json:"propainter"`
	Grain       bool   `json:"grain"`
	OCR         bool   `json:"ocr"`
	CRF         int    `json:"crf"`
	ForceEngine string `json:"force_engine"`
	PPConcurrency int  `json:"pp_concurrency"`
	DiffuEraser bool   `json:"diffueraser"`
}

// args converts the snapshot into desub remove flags.
func (p Params) args() []string {
	var out []string
	if p.Propainter {
		out = append(out, "--propainter")
	}
	if p.Grain {
		out = append(out, "--grain")
	}
	if p.OCR {
		out = append(out, "--ocr")
	}
	if p.CRF > 0 {
		out = append(out, "--crf", strconv.Itoa(p.CRF))
	}
	if p.ForceEngine != "" {
		out = append(out, "--force-engine", p.ForceEngine)
	}
	if p.PPConcurrency > 1 {
		out = append(out, "--pp-concurrency", strconv.Itoa(p.PPConcurrency))
	}
	return out
}

// Run executes `desub remove` on srcPath inside the container, writing the
// result to workDir/outName, and streams parsed events. ctx cancellation
// kills the container.
func Run(ctx context.Context, o DockerOptions, workDir, srcPath, outName, paramsJSON string, events chan<- Event) error {
	o.defaults()
	var p Params
	if paramsJSON != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &p); err != nil {
			return fmt.Errorf("任务参数快照损坏: %v", err)
		}
	}
	if err := stageInput(workDir, srcPath); err != nil {
		return err
	}
	image := o.Image
	painterScript := "/src/scripts/propainter_infer.py"
	args := []string{"run", "--rm", "--name", containerName(workDir)}
	if o.GPUs != "" {
		args = append(args, "--gpus", o.GPUs)
	}
	args = append(args,
		"-v", filepath.Join(o.Lab) + `:/work`,
		"-v", filepath.Join(o.Repo) + `:/src`,
		"-v", filepath.Join(o.Lab, `docker\paddlex`) + `:/root/.paddlex`,
		"-v", workDir + `:/task`,
		"-w", "/work",
		"-e", "HTTP_PROXY="+o.Proxy,
		"-e", "HTTPS_PROXY="+o.Proxy,
		"-e", "NO_PROXY=host.docker.internal,127.0.0.1,localhost",
		"-e", "PROPAINTER_HOME=/work/vendor/ProPainter",
		"-e", "PADDLE_PDX_DISABLE_MODEL_SOURCE_CHECK=True",
	)
	if p.DiffuEraser {
		image = "desub:diffueraser"
		painterScript = "/src/scripts/diffueraser_infer.py"
		args = append(args, "-e", "DIFFUERASER_HOME=/work/vendor/DiffuEraser")
	}
	args = append(args,
		image, o.Bin, "remove", "/task/input.mp4",
		"-o", "/task/"+outName,
		"--propainter-script", painterScript,
	)
	if p.OCR {
		args = append(args, "--ocr-script", "/src/scripts/ocr_boxes.py")
	}
	args = append(args, p.args()...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // desub logs to stdout; merge sidecar noise
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("docker start: %v", err)
	}
	go func() { // ctx cancel -> kill the container, not just the client
		<-ctx.Done()
		exec.Command("docker", "kill", containerName(workDir)).Run()
	}()
	parseStream(stdout, events)
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("desub 退出非零: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, outName)); err != nil {
		return fmt.Errorf("desub 未产出 %s", outName)
	}
	return nil
}

func containerName(workDir string) string {
	return "desub-task-" + filepath.Base(workDir)
}

// stageInput links (or copies, across volumes) the chosen video into the
// task work dir so the container sees it at the fixed path /task/input.mp4.
func stageInput(workDir, srcPath string) error {
	dst := filepath.Join(workDir, "input.mp4")
	if _, err := os.Stat(dst); err == nil {
		return nil // 重跑复用
	}
	if err := os.Link(srcPath, dst); err == nil {
		return nil
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("复制输入视频失败: %v", err)
	}
	return nil
}

var (
	reStageProbe  = regexp.MustCompile(`^video \d+x\d+`)
	reStageCuts   = regexp.MustCompile(`^scene cuts:`)
	reStageDetect = regexp.MustCompile(`^detect: \d+ frames`)
	reStageOCR    = regexp.MustCompile(`^ocr: `)
	reStageEngine = regexp.MustCompile(`^engine \S+.* -> `)
	reStageVerify = regexp.MustCompile(`^verify: `)
	reProgress    = regexp.MustCompile(`^repair (\d+)/(\d+)$`)
)

// parseStream consumes desub stdout: lines and \r-separated progress ticks.
// The trailing pretty-printed JSON report is captured as one "report" event.
func parseStream(r io.Reader, events chan<- Event) {
	br := bufio.NewReader(r)
	var jsDepth int
	var jsBuf strings.Builder
	inJSON := false
	flush := func(tok string) {
		if tok == "" {
			return
		}
		if inJSON {
			jsBuf.WriteString(tok)
			jsDepth += strings.Count(tok, "{") - strings.Count(tok, "}")
			if jsDepth <= 0 {
				inJSON = false
				events <- Event{Type: "report", Msg: jsBuf.String()}
				jsBuf.Reset()
			}
			return
		}
		if tok == "{" {
			inJSON, jsDepth = true, 1
			jsBuf.WriteString(tok)
			return
		}
		if m := reProgress.FindStringSubmatch(tok); m != nil {
			done, _ := strconv.Atoi(m[1])
			total, _ := strconv.Atoi(m[2])
			events <- Event{Type: "progress", Done: done, Total: total}
			return
		}
		switch {
		case reStageProbe.MatchString(tok):
			events <- Event{Type: "stage", Stage: "probe"}
		case reStageCuts.MatchString(tok):
			events <- Event{Type: "stage", Stage: "cuts"}
		case reStageDetect.MatchString(tok):
			events <- Event{Type: "stage", Stage: "detect"}
		case reStageEngine.MatchString(tok):
			events <- Event{Type: "stage", Stage: "engine"}
		case reStageVerify.MatchString(tok):
			events <- Event{Type: "stage", Stage: "verify"}
		}
		events <- Event{Type: "log", Msg: tok}
	}
	for {
		tok, err := readToken(br)
		flush(tok)
		if err != nil {
			return
		}
	}
}

// readToken reads until \r or \n (repair ticks overwrite in place with \r).
func readToken(br *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		c, err := br.ReadByte()
		if err != nil {
			return sb.String(), err
		}
		if c == '\r' || c == '\n' {
			if c == '\r' {
				if b, err := br.Peek(1); err == nil && b[0] == '\n' {
					br.ReadByte()
				}
			}
			return sb.String(), nil
		}
		sb.WriteByte(c)
	}
}
