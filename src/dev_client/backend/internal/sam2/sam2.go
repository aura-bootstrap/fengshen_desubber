// Package sam2 drives the SAM2 mask-refinement sidecar
// (scripts/sam2_masks.py): stroke-level masks are upgraded to pixel-level
// segmentation propagated over each shot. Refined masks stay gated to the
// neighbourhood of the originals, so a refinement failure or over-segmentation
// never widens the repair area beyond the dilation gate; any segment that
// fails keeps its original masks.
package sam2

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/aura-bootstrap/fengshen_desubber/internal/ffx"
	"github.com/aura-bootstrap/fengshen_desubber/internal/mask"
)

// Client runs one refinement sidecar per shot segment.
type Client struct {
	Script  string
	Python  string
	Timeout time.Duration
	Home    string // SAM2_HOME passed to the sidecar subprocess
}

func NewClient(script string, timeout time.Duration) (*Client, error) {
	if script == "" {
		script = "scripts/sam2_masks.py"
	}
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("sam2: %v", err)
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &Client{Script: script, Python: "python3", Timeout: timeout}, nil
}

// Refine upgrades masks (band coords, one per input frame) via SAM2 video
// propagation, shot by shot. Segments whose refinement fails keep their
// original masks.
func (c *Client) Refine(input string, w, bandY, bandH int, fps float64, masks []mask.Frame, cuts []float64, log io.Writer) ([]mask.Frame, error) {
	n := len(masks)
	if n == 0 {
		return masks, nil
	}
	cutF := []int{}
	for _, c := range cuts {
		cutF = append(cutF, int(c*fps+0.5))
	}
	sort.Ints(cutF)
	type seg struct{ s, e int }
	var segs []seg
	s := 0
	for _, c := range cutF {
		if c > s && c <= n {
			segs = append(segs, seg{s, c - 1})
			s = c
		}
	}
	segs = append(segs, seg{s, n - 1})

	out := make([]mask.Frame, n)
	copy(out, masks)
	jobDir, err := os.MkdirTemp("", "sam2-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(jobDir)

	todo := make([]seg, 0, len(segs))
	for _, sg := range segs {
		for k := sg.s; k <= sg.e; k++ {
			if !masks[k].Empty() {
				todo = append(todo, sg)
				break
			}
		}
	}
	fmt.Fprintf(log, "sam2: refining masks in %d segment(s)\n", len(todo))
	done := 0
	for _, sg := range todo {
		ref, err := c.refineSegment(jobDir, input, w, bandY, bandH, fps, masks[sg.s:sg.e+1], sg.s)
		if err != nil {
			fmt.Fprintf(log, "warn: sam2 segment %d-%d failed (%v); keeping stroke masks\n", sg.s, sg.e, err)
			continue
		}
		for k := sg.s; k <= sg.e; k++ {
			out[k] = ref[k-sg.s]
		}
		done++
		fmt.Fprintf(log, "sam2 %d/%d\r", done, len(todo))
	}
	return out, nil
}

func (c *Client) refineSegment(jobDir, input string, w, bandY, bandH int, fps float64, masks []mask.Frame, segStart int) ([]mask.Frame, error) {
	n := len(masks)
	dir := filepath.Join(jobDir, fmt.Sprintf("seg-%05d", segStart))
	stripDir := filepath.Join(dir, "strip")
	maskDir := filepath.Join(dir, "masks")
	outDir := filepath.Join(dir, "out")
	for _, d := range []string{stripDir, maskDir, outDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.6f", float64(segStart)/fps),
		"-i", input,
		"-vf", fmt.Sprintf("crop=%d:%d:0:%d", w, bandH, bandY),
		"-frames:v", fmt.Sprint(n),
		"-start_number", "0",
		filepath.Join(stripDir, "%05d.png")}
	cmd := exec.Command(ffx.FFmpeg(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("strip export: %v: %s", err, tail(stderr.String(), 300))
	}
	if _, err := os.Stat(filepath.Join(stripDir, frameName(n-1))); err != nil {
		return nil, fmt.Errorf("strip export: %v", err)
	}

	bits := make([]uint8, w*bandH)
	for k, m := range masks {
		for i := range bits {
			bits[i] = 0
		}
		if !m.Empty() {
			m.Decode(bits, w)
		}
		img := image.NewGray(image.Rect(0, 0, w, bandH))
		for p, b := range bits {
			if b != 0 {
				img.Pix[p] = 255
			}
		}
		f, err := os.Create(filepath.Join(maskDir, frameName(k)))
		if err != nil {
			return nil, err
		}
		err = png.Encode(f, img)
		f.Close()
		if err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	cmd = exec.CommandContext(ctx, c.Python, c.Script,
		"--strip", stripDir, "--masks", maskDir, "--out", outDir,
		"--start", "0", "--count", fmt.Sprint(n))
	if c.Home != "" {
		cmd.Env = append(os.Environ(), "SAM2_HOME="+c.Home)
	}
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sidecar: %v: %s", err, tail(stderr.String(), 400))
	}

	out := make([]mask.Frame, n)
	for k := 0; k < n; k++ {
		f, err := os.Open(filepath.Join(outDir, frameName(k)))
		if err != nil {
			return nil, fmt.Errorf("read refined mask %d: %v", k, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("decode refined mask %d: %v", k, err)
		}
		b := img.Bounds()
		if b.Dx() != w || b.Dy() != bandH {
			return nil, fmt.Errorf("refined mask %d shape %v, want %dx%d", k, b, w, bandH)
		}
		nb := make([]uint8, w*bandH)
		for y := 0; y < bandH; y++ {
			for x := 0; x < w; x++ {
				r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if r > 0x7fff {
					nb[y*w+x] = 1
				}
			}
		}
		out[k] = mask.Encode(nb, w, bandH)
	}
	return out, nil
}

func frameName(i int) string { return fmt.Sprintf("%05d.png", i) }

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
