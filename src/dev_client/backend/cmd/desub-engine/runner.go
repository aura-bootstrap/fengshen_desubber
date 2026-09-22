package main

// 本地子进程执行器:把任务固化的配置快照映射成 `desub remove` 旗标,
// 拉起身旁的 desub.exe,stdout 解析成 stage/progress/report 事件。
// ctx 取消即杀进程(含 ffmpeg 子进程树由 desub 自身清理)。
// 注意:runLocal 不得关闭 events——done 事件由 startTaskRun 的 goroutine 在
// runLocal 返回后发送,由它统一 defer close(此处关闭会让 done 发送到
// 已关闭通道,panic 崩掉整个引擎)。

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

	"github.com/aura-bootstrap/fengshen_desubber/internal/store"
)

// args 把配置快照转成 desub remove 旗标(与 cmd/desub 的旗标一一对应)。
func (c *Config) args() []string {
	var out []string
	out = append(out,
		"--engine", c.Repair.Engine,
		"--band", strconv.FormatFloat(c.Detect.BandStart, 'f', -1, 64),
		"--crf", strconv.Itoa(c.Encode.CRF),
		"--preset", c.Encode.Preset,
		"--max-gap", strconv.Itoa(c.Detect.MaxGap),
		"--scene-thr", strconv.FormatFloat(c.Detect.SceneThreshold, 'f', -1, 64),
		"--neighbors", strconv.Itoa(c.Repair.Neighbors),
		"--pad", strconv.Itoa(c.Repair.Pad),
		"--close-gap", strconv.Itoa(c.Detect.CloseGap),
		"--close-overlap", strconv.FormatFloat(c.Detect.CloseOverlap, 'f', -1, 64),
		"--edge-pad", strconv.Itoa(c.Detect.EdgePad),
		"--risk-coverage", strconv.FormatFloat(c.Output.RiskCoverage, 'f', -1, 64),
		"--verify="+strconv.FormatBool(c.Output.Verify),
	)
	if !c.Repair.Motion {
		out = append(out, "--no-motion")
	}
	if c.Repair.Grain {
		out = append(out, "--grain")
	}
	if c.Repair.ForceEngine != "" {
		out = append(out, "--force-engine", c.Repair.ForceEngine)
	}
	if c.Detect.Alpha {
		out = append(out, "--alpha")
	}
	if c.Detect.OCR {
		out = append(out, "--ocr", "--ocr-stride", strconv.Itoa(c.Detect.OCRStride))
	}
	if c.Enhance.ProPainter {
		out = append(out, "--propainter",
			"--pp-mask-dilation", strconv.Itoa(c.Enhance.PPMaskDilation),
			"--pp-tight-dilate", strconv.Itoa(c.Enhance.PPTightDilate),
			"--pp-raft-iter", strconv.Itoa(c.Enhance.PPRaftIter),
			"--pp-neighbor-length", strconv.Itoa(c.Enhance.PPNeighborLength),
			"--pp-concurrency", strconv.Itoa(c.Enhance.PPConcurrency),
		)
		// 生成式旁车选择:空/propainter 用 desub 默认脚本,其余显式指定
		// (脚本路径相对安装目录,runLocal 已把 cwd 锚到 exeDir)。
		switch c.Enhance.Painter {
		case "diffueraser":
			out = append(out, "--propainter-script", "scripts/diffueraser_infer.py")
		case "wanvace":
			out = append(out, "--propainter-script", "scripts/wanvace_infer.py")
		}
	}
	if c.Enhance.SAM2 {
		out = append(out, "--sam2")
	}
	if c.Enhance.FaceRestore {
		out = append(out, "--face-restore")
	}
	if c.Enhance.VLMQC {
		out = append(out, "--vlm-qc")
	}
	return out
}

// runLocal 执行一次 `desub remove`,产物写到任务工作区,流式回报事件。
func (s *server) runLocal(ctx context.Context, tk *store.Task, events chan<- Event) error {
	cfg := defaultConfig()
	if tk.ParamsJSON != "" {
		if err := json.Unmarshal([]byte(tk.ParamsJSON), cfg); err != nil {
			return fmt.Errorf("任务参数快照损坏: %v", err)
		}
	}
	if cfg.Online.Enabled {
		// 在线分支:不拉本地 desub.exe,上传原片给计费服务云端处理(见 online.go)。
		return runOnline(ctx, s.exeDir, tk.WorkDir, tk.SrcPath, tk.OutName, events)
	}
	bin := filepath.Join(s.exeDir, "desub.exe")
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("找不到引擎 CLI: %s", bin)
	}
	args := []string{"remove", tk.SrcPath, "-o", filepath.Join(tk.WorkDir, tk.OutName)}
	args = append(args, cfg.args()...)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = s.exeDir // scripts/ 等相对路径锚定安装目录
	// 安装目录自带 bin/ffmpeg 时优先使用(无则回落系统 PATH)
	if ff := filepath.Join(s.exeDir, "bin", "ffmpeg.exe"); fileExists(ff) {
		cmd.Env = append(os.Environ(),
			"DESUB_FFMPEG="+ff,
			"DESUB_FFPROBE="+filepath.Join(s.exeDir, "bin", "ffprobe.exe"),
		)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // desub 日志走 stderr,合并解析
	events <- Event{Type: "log", Msg: "执行: desub " + joinArgs(args)}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("引擎启动失败: %v", err)
	}
	parseStream(stdout, events)
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("desub 退出非零: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tk.WorkDir, tk.OutName)); err != nil {
		return fmt.Errorf("desub 未产出 %s", tk.OutName)
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

var (
	reStageProbe  = regexp.MustCompile(`^video \d+x\d+`)
	reStageCuts   = regexp.MustCompile(`^scene cuts:`)
	reStageDetect = regexp.MustCompile(`^detect: \d+ frames`)
	reStageOCR    = regexp.MustCompile(`^ocr: `)
	reStageEngine = regexp.MustCompile(`^engine \S+.* -> `)
	reStageVerify = regexp.MustCompile(`^verify: `)
	reProgress    = regexp.MustCompile(`^(?:repair|sam2) (\d+)/(\d+)$`)
)

// parseStream consumes desub stdout: lines and \r-separated progress ticks.
// The trailing pretty-printed JSON report is captured as one "report" event.
func parseStream(r io.Reader, events chan<- Event) {
	br := bufio.NewReader(r)
	var jsDepth int
	var jsBuf []byte
	inJSON := false
	flush := func(tok string) {
		if tok == "" {
			return
		}
		if inJSON {
			jsBuf = append(jsBuf, tok...)
			for _, c := range tok {
				switch c {
				case '{':
					jsDepth++
				case '}':
					jsDepth--
				}
			}
			if jsDepth <= 0 {
				inJSON = false
				events <- Event{Type: "report", Msg: string(jsBuf)}
				jsBuf = nil
			}
			return
		}
		if tok == "{" {
			inJSON, jsDepth = true, 1
			jsBuf = append(jsBuf[:0], tok...)
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
		case reStageOCR.MatchString(tok):
			events <- Event{Type: "stage", Stage: "ocr"}
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
	var sb []byte
	for {
		c, err := br.ReadByte()
		if err != nil {
			return string(sb), err
		}
		if c == '\r' || c == '\n' {
			if c == '\r' {
				if b, err := br.Peek(1); err == nil && b[0] == '\n' {
					br.ReadByte()
				}
			}
			return string(sb), nil
		}
		sb = append(sb, c)
	}
}
