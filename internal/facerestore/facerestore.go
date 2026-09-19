// Package facerestore wraps a Painter with a GFPGAN face-prior post-pass
// (scripts/face_restore.py): faces intersecting the repair zone are restored
// after inpainting. Any failure falls back to the inner painter's output.
package facerestore

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
	"time"

	"github.com/aura-bootstrap/fengshen_desubber/internal/engine"
)

// Painter wraps another engine.Painter and runs the face-restore sidecar on
// its output frames inside the repair zone.
type Painter struct {
	Inner   engine.Painter
	Script  string
	Python  string
	Timeout time.Duration
	Log     io.Writer
}

func (p *Painter) Inpaint(j engine.PaintJob) ([][]byte, error) {
	frames, err := p.Inner.Inpaint(j)
	if err != nil {
		return nil, err
	}
	script := p.Script
	if script == "" {
		script = "scripts/face_restore.py"
	}
	if _, err := os.Stat(script); err != nil {
		p.logf("warn: facerestore disabled: %v\n", err)
		return frames, nil
	}
	python := p.Python
	if python == "" {
		python = "python3"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	dir := j.WorkDir
	if dir == "" {
		dir = os.TempDir()
	}
	jobDir, err := os.MkdirTemp(dir, "facerestore-*")
	if err != nil {
		p.logf("warn: facerestore: %v\n", err)
		return frames, nil
	}
	defer os.RemoveAll(jobDir)
	stripDir := filepath.Join(jobDir, "strip")
	maskDir := filepath.Join(jobDir, "masks")
	if err := os.MkdirAll(stripDir, 0o755); err != nil {
		p.logf("warn: facerestore: %v\n", err)
		return frames, nil
	}
	if err := os.MkdirAll(maskDir, 0o755); err != nil {
		p.logf("warn: facerestore: %v\n", err)
		return frames, nil
	}

	w, h := j.W, j.BandH
	bits := make([]uint8, w*h)
	for i, fr := range frames {
		img := &image.RGBA{Pix: rgbToRGBA(fr), Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		if err := writePNG(filepath.Join(stripDir, frameName(i)), img); err != nil {
			p.logf("warn: facerestore: %v\n", err)
			return frames, nil
		}
		mg := image.NewGray(image.Rect(0, 0, w, h))
		if i < len(j.Masks) && !j.Masks[i].Empty() {
			j.Masks[i].Decode(bits, w)
			for q, b := range bits {
				if b != 0 {
					mg.Pix[q] = 255
				}
			}
		}
		for q := range bits {
			bits[q] = 0
		}
		if err := writePNG(filepath.Join(maskDir, frameName(i)), mg); err != nil {
			p.logf("warn: facerestore: %v\n", err)
			return frames, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script,
		"--strip", stripDir, "--masks", maskDir,
		"--start", "0", "--count", fmt.Sprint(len(frames)))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		p.logf("warn: facerestore sidecar: %v: %s\n", err, tail(stderr.String(), 300))
		return frames, nil
	}

	for i := range frames {
		f, err := os.Open(filepath.Join(stripDir, frameName(i)))
		if err != nil {
			p.logf("warn: facerestore read back: %v\n", err)
			return frames, nil
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			p.logf("warn: facerestore decode: %v\n", err)
			return frames, nil
		}
		frames[i] = rgbOf(img)
	}
	return frames, nil
}

func (p *Painter) logf(format string, args ...any) {
	if p.Log != nil {
		fmt.Fprintf(p.Log, format, args...)
	}
}

func rgbToRGBA(rgb []byte) []byte {
	n := len(rgb) / 3
	out := make([]byte, n*4)
	for i := 0; i < n; i++ {
		out[i*4] = rgb[i*3]
		out[i*4+1] = rgb[i*3+1]
		out[i*4+2] = rgb[i*3+2]
		out[i*4+3] = 255
	}
	return out
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func rgbOf(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			p := (y*w + x) * 3
			out[p] = uint8(r >> 8)
			out[p+1] = uint8(g >> 8)
			out[p+2] = uint8(bl >> 8)
		}
	}
	return out
}

func frameName(i int) string { return fmt.Sprintf("%05d.png", i) }

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
