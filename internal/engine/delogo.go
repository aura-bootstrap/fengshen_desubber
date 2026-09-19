// Package engine implements the removal backends. Both consume detections
// and write a new file; audio is stream-copied, video is re-encoded once.
package engine

import (
	"fmt"
	"strings"

	"github.com/aura-bootstrap/fengshen_desubber/internal/events"
	"github.com/aura-bootstrap/fengshen_desubber/internal/ffx"
	"github.com/aura-bootstrap/fengshen_desubber/internal/imgx"
)

type DelogoOptions struct {
	Input  string
	Output string
	W, H   int
	Events []events.Event // full-frame coordinates
	Pad    int
	CRF    int
	Preset string
	// EncColor carries ffx.MediaInfo.ColorEncodeArgs() output (R9.3).
	EncColor []string
	// EncCodec/EncPixFmt/EncHDR carry ffx.MediaInfo.EncodeProfile() output.
	EncCodec  string
	EncPixFmt string
	EncHDR    []string
}

// RunDelogo builds one ffmpeg filtergraph with a timed delogo box per event.
func RunDelogo(o DelogoOptions) error {
	const maxBoxes = 400
	var parts []string
	for _, ev := range o.Events {
		r := ev.Box.Expand(o.Pad).Clamp(o.W, o.H)
		r = r.Intersect(imgx.Rect{X: 1, Y: 1, W: o.W - 2, H: o.H - 2})
		if r.W < 6 || r.H < 6 {
			continue
		}
		parts = append(parts, fmt.Sprintf("delogo=x=%d:y=%d:w=%d:h=%d:enable='between(t,%.3f,%.3f)'",
			r.X, r.Y, r.W, r.H, ev.Start, ev.End))
	}
	if len(parts) == 0 {
		return fmt.Errorf("delogo: no usable events")
	}
	if len(parts) > maxBoxes {
		return fmt.Errorf("delogo: %d boxes exceeds single-pass limit (%d), split the input", len(parts), maxBoxes)
	}
	codec, pixFmt := o.EncCodec, o.EncPixFmt
	if codec == "" {
		codec = "libx264"
	}
	if pixFmt == "" {
		pixFmt = "yuv420p"
	}
	threads := ffx.CPUWorkers()
	if codec == "libx265" && threads > 16 {
		threads = 16 // x265 hard-caps frame-threads at 16
	}
	vf := strings.Join(parts, ",") + ",format=" + pixFmt
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-i", o.Input, "-vf", vf,
		"-map", "0:v:0", "-map", "0:a?"}
	args = append(args, o.EncColor...)
	args = append(args, o.EncHDR...)
	args = append(args,
		"-c:v", codec, "-crf", fmt.Sprint(o.CRF), "-preset", o.Preset,
		"-threads", fmt.Sprint(threads),
		"-pix_fmt", pixFmt, "-c:a", "copy", "-movflags", "+faststart", o.Output)
	return ffx.Run(args...)
}
