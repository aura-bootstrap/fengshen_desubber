// Package ffx wraps ffmpeg/ffprobe subprocesses. All media IO (decode,
// encode, probing, scene detection) happens inside ffmpeg; this package
// only pumps raw frames through pipes.
package ffx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func FFmpeg() string  { return binName("ffmpeg") }
func FFprobe() string { return binName("ffprobe") }

// CPUWorkers returns the default concurrency for Go worker pools and ffmpeg
// -threads: 80% of the logical cores (DESUB_CPU_PCT overrides the
// percentage), capped at 25, at least 1.
func CPUWorkers() int {
	pct := 80
	if v, err := strconv.Atoi(os.Getenv("DESUB_CPU_PCT")); err == nil && v > 0 && v <= 100 {
		pct = v
	}
	w := runtime.NumCPU() * pct / 100
	if w > 25 {
		w = 25
	}
	if w < 1 {
		w = 1
	}
	return w
}

func binName(def string) string {
	if v := os.Getenv("DESUB_" + strings.ToUpper(def)); v != "" {
		return v
	}
	return def
}

// MasteringDisplay carries HDR10 static metadata in x265 master-display
// units: chromaticity in 1/50000, luminance in 1/10000 cd/m².
type MasteringDisplay struct {
	Rx, Ry, Gx, Gy, Bx, By, Wx, Wy int
	MaxLum, MinLum                 int
}

type MediaInfo struct {
	Path            string  `json:"path"`
	W               int     `json:"width"`
	H               int     `json:"height"`
	FPS             float64 `json:"fps"`
	Duration        float64 `json:"duration"`
	Rotation        int     `json:"rotation"`
	VCodec          string  `json:"vcodec"`
	PixFmt          string  `json:"pix_fmt,omitempty"`
	BitDepth        int     `json:"bit_depth,omitempty"`
	ColorRange      string  `json:"color_range,omitempty"`
	ColorSpace      string  `json:"color_space,omitempty"`
	ColorTransfer   string  `json:"color_transfer,omitempty"`
	ColorPrimaries  string  `json:"color_primaries,omitempty"`
	MasterDisplay   *MasteringDisplay `json:"master_display,omitempty"`
	MaxCLL          int     `json:"max_cll,omitempty"`
	MaxFALL         int     `json:"max_fall,omitempty"`
	DoVi            bool    `json:"dovi,omitempty"`
	HasAudio        bool    `json:"has_audio"`
	SubtitleStreams int     `json:"subtitle_streams"`
	NBytes          int64   `json:"bytes"`
}

// ColorEncodeArgs returns encoder flags reproducing the input's colour
// metadata on the output (R9.3). Unset or "unknown" fields are omitted so the
// muxer default applies.
func (mi *MediaInfo) ColorEncodeArgs() []string {
	var args []string
	add := func(flag, v string) {
		if v != "" && v != "unknown" {
			args = append(args, flag, v)
		}
	}
	add("-color_range", mi.ColorRange)
	add("-colorspace", mi.ColorSpace)
	add("-color_trc", mi.ColorTransfer)
	add("-color_primaries", mi.ColorPrimaries)
	return args
}

// IsHDR reports whether the source carries an HDR signal: HDR10 static
// metadata, or a PQ/HLG transfer characteristic.
func (mi *MediaInfo) IsHDR() bool {
	return mi.MasterDisplay != nil ||
		mi.ColorTransfer == "smpte2084" || mi.ColorTransfer == "arib-std-b67"
}

// EncodeProfile picks the output codec, pixel format and extra encoder flags
// so the finished file keeps the source's colour fidelity (R9.3). SDR stays
// on libx264/yuv420p; HDR sources switch to libx265 10-bit because the
// bundled x264 build silently drops mastering-display SEI, and emit the
// source's HDR10 static metadata. Dolby Vision RPU cannot survive a
// re-encode; callers should warn when DoVi is set.
func (mi *MediaInfo) EncodeProfile() (codec, pixFmt string, extra []string) {
	codec, pixFmt = "libx264", "yuv420p"
	if mi.BitDepth >= 10 || mi.IsHDR() {
		codec, pixFmt = "libx265", "yuv420p10le"
		extra = append(extra, "-tag:v", "hvc1")
		if md := mi.MasterDisplay; md != nil {
			extra = append(extra, "-x265-params", fmt.Sprintf(
				"master-display=G(%d,%d)B(%d,%d)R(%d,%d)WP(%d,%d)L(%d,%d):max-cll=%d,%d",
				md.Gx, md.Gy, md.Bx, md.By, md.Rx, md.Ry, md.Wx, md.Wy,
				md.MaxLum, md.MinLum, mi.MaxCLL, mi.MaxFALL))
		} else if mi.MaxCLL > 0 {
			extra = append(extra, "-x265-params", fmt.Sprintf("max-cll=%d,%d", mi.MaxCLL, mi.MaxFALL))
		}
	}
	return codec, pixFmt, extra
}

type rawProbe struct {
	Streams []struct {
		Index            int    `json:"index"`
		CodecType        string `json:"codec_type"`
		CodecName        string `json:"codec_name"`
		Width            int    `json:"width"`
		Height           int    `json:"height"`
		PixFmt           string `json:"pix_fmt"`
		BitsPerRawSample string `json:"bits_per_raw_sample"`
		ColorRange       string `json:"color_range"`
		ColorSpace       string `json:"color_space"`
		ColorTransfer    string `json:"color_transfer"`
		ColorPrimaries   string `json:"color_primaries"`
		AvgFrameRate     string `json:"avg_frame_rate"`
		RFrameRate       string `json:"r_frame_rate"`
		Duration         string `json:"duration"`
		SideDataList     []struct {
			SideDataType string `json:"side_data_type"`
			Rotation     float64 `json:"rotation"`
			// Mastering display metadata: chromaticity as "n/50000"
			// rationals, luminance as "n/10000".
			RedX         string `json:"red_x"`
			RedY         string `json:"red_y"`
			GreenX       string `json:"green_x"`
			GreenY       string `json:"green_y"`
			BlueX        string `json:"blue_x"`
			BlueY        string `json:"blue_y"`
			WhiteX       string `json:"white_point_x"`
			WhiteY       string `json:"white_point_y"`
			MinLuminance string `json:"min_luminance"`
			MaxLuminance string `json:"max_luminance"`
			MaxContent   int    `json:"max_content"`
			MaxAverage   int    `json:"max_average"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
		Size     string `json:"size"`
	} `json:"format"`
}

func Probe(path string) (*MediaInfo, error) {
	args := []string{"-v", "error", "-print_format", "json", "-show_streams", "-show_format", path}
	out, err := exec.Command(FFprobe(), args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var rp rawProbe
	if err := json.Unmarshal(out, &rp); err != nil {
		return nil, fmt.Errorf("ffprobe json: %w", err)
	}
	mi := &MediaInfo{Path: path}
	for _, s := range rp.Streams {
		switch s.CodecType {
		case "video":
			if mi.W != 0 {
				continue
			}
			w, h, rot := s.Width, s.Height, 0
			var md *MasteringDisplay
			cll, fall := 0, 0
			for _, sd := range s.SideDataList {
				switch {
				case sd.Rotation != 0:
					rot = int(sd.Rotation)
				case sd.SideDataType == "Mastering display metadata":
					md = &MasteringDisplay{
						Rx: ratNum(sd.RedX, 50000), Ry: ratNum(sd.RedY, 50000),
						Gx: ratNum(sd.GreenX, 50000), Gy: ratNum(sd.GreenY, 50000),
						Bx: ratNum(sd.BlueX, 50000), By: ratNum(sd.BlueY, 50000),
						Wx: ratNum(sd.WhiteX, 50000), Wy: ratNum(sd.WhiteY, 50000),
						MaxLum: ratNum(sd.MaxLuminance, 10000),
						MinLum: ratNum(sd.MinLuminance, 10000),
					}
				case sd.SideDataType == "Content light level metadata":
					cll, fall = sd.MaxContent, sd.MaxAverage
				case strings.Contains(sd.SideDataType, "DOVI"):
					mi.DoVi = true
				}
			}
			rot = ((rot % 360) + 360) % 360
			if rot == 90 || rot == 270 {
				w, h = h, w
			}
			mi.W, mi.H, mi.Rotation = w, h, rot
			mi.VCodec = s.CodecName
			mi.PixFmt = s.PixFmt
			mi.BitDepth = probeBitDepth(s.BitsPerRawSample, s.PixFmt)
			mi.ColorRange = s.ColorRange
			mi.ColorSpace = s.ColorSpace
			mi.ColorTransfer = s.ColorTransfer
			mi.ColorPrimaries = s.ColorPrimaries
			mi.MasterDisplay = md
			mi.MaxCLL, mi.MaxFALL = cll, fall
			mi.FPS = parseFPS(s.AvgFrameRate)
			if mi.FPS == 0 {
				mi.FPS = parseFPS(s.RFrameRate)
			}
		case "audio":
			mi.HasAudio = true
		case "subtitle":
			mi.SubtitleStreams++
		}
	}
	if mi.W == 0 {
		return nil, fmt.Errorf("no video stream in %s", path)
	}
	if v, err := strconv.ParseFloat(rp.Format.Duration, 64); err == nil {
		mi.Duration = v
	}
	if v, err := strconv.ParseInt(rp.Format.Size, 10, 64); err == nil {
		mi.NBytes = v
	}
	if err := probeFrameSideData(path, mi); err != nil {
		// Frame-level probing is best-effort: stream-level metadata alone is
		// enough to keep going.
		_ = err
	}
	return mi, nil
}

// probeFrameSideData reads the first video frame's SEI side data: x265-written
// HDR10 mastering/CLL SEI only surfaces at frame level, not stream level.
func probeFrameSideData(path string, mi *MediaInfo) error {
	args := []string{"-v", "error", "-print_format", "json", "-select_streams", "v:0",
		"-show_frames", "-read_intervals", "%+#1", path}
	out, err := exec.Command(FFprobe(), args...).Output()
	if err != nil {
		return err
	}
	var fp struct {
		Frames []struct {
			SideDataList []struct {
				SideDataType string `json:"side_data_type"`
				RedX         string `json:"red_x"`
				RedY         string `json:"red_y"`
				GreenX       string `json:"green_x"`
				GreenY       string `json:"green_y"`
				BlueX        string `json:"blue_x"`
				BlueY        string `json:"blue_y"`
				WhiteX       string `json:"white_point_x"`
				WhiteY       string `json:"white_point_y"`
				MinLuminance string `json:"min_luminance"`
				MaxLuminance string `json:"max_luminance"`
				MaxContent   int    `json:"max_content"`
				MaxAverage   int    `json:"max_average"`
			} `json:"side_data_list"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(out, &fp); err != nil {
		return err
	}
	if len(fp.Frames) == 0 {
		return nil
	}
	for _, sd := range fp.Frames[0].SideDataList {
		switch {
		case sd.SideDataType == "Mastering display metadata" && mi.MasterDisplay == nil:
			mi.MasterDisplay = &MasteringDisplay{
				Rx: ratNum(sd.RedX, 50000), Ry: ratNum(sd.RedY, 50000),
				Gx: ratNum(sd.GreenX, 50000), Gy: ratNum(sd.GreenY, 50000),
				Bx: ratNum(sd.BlueX, 50000), By: ratNum(sd.BlueY, 50000),
				Wx: ratNum(sd.WhiteX, 50000), Wy: ratNum(sd.WhiteY, 50000),
				MaxLum: ratNum(sd.MaxLuminance, 10000),
				MinLum: ratNum(sd.MinLuminance, 10000),
			}
		case sd.SideDataType == "Content light level metadata" && mi.MaxCLL == 0:
			mi.MaxCLL, mi.MaxFALL = sd.MaxContent, sd.MaxAverage
		case strings.Contains(sd.SideDataType, "DOVI"):
			mi.DoVi = true
		}
	}
	return nil
}

// ratNum parses an ffprobe rational ("34000/50000") and rescales the
// numerator to the target denominator; plain ints pass through.
func ratNum(s string, targetDen int) int {
	if s == "" {
		return 0
	}
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		v, _ := strconv.Atoi(s)
		return v
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return int(n * float64(targetDen) / d)
}

// probeBitDepth reads bits_per_raw_sample, falling back to the pix_fmt
// suffix ("yuv420p10le" → 10).
func probeBitDepth(bitsRaw, pixFmt string) int {
	if v, err := strconv.Atoi(bitsRaw); err == nil && v > 0 {
		return v
	}
	for _, suf := range []string{"12le", "12be", "10le", "10be", "9le", "9be"} {
		if strings.HasSuffix(pixFmt, suf) {
			v, _ := strconv.Atoi(suf[:2])
			return v
		}
	}
	return 8
}

func parseFPS(s string) float64 {
	if s == "" || s == "0/0" {
		return 0
	}
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}

// BytesPer returns the byte size of one pixel for a rawvideo pix_fmt.
func BytesPer(pixfmt string) int {
	switch pixfmt {
	case "gray":
		return 1
	case "rgb24":
		return 3
	case "rgba":
		return 4
	}
	return 1
}

// GrabFrame decodes a single frame at time t through vf and returns it as
// rawvideo (pixfmt, w×h).
func GrabFrame(input string, t float64, vf string, w, h int, pixfmt string) ([]byte, error) {
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error",
		"-ss", strconv.FormatFloat(t, 'f', 6, 64), "-i", input, "-an", "-sn", "-dn"}
	if vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", pixfmt, "pipe:1")
	cmd := exec.Command(FFmpeg(), args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg grab: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	if want := w * h * BytesPer(pixfmt); len(out) != want {
		return nil, fmt.Errorf("ffmpeg grab: got %d bytes, want %d", len(out), want)
	}
	return out, nil
}

// FrameReader streams raw frames out of ffmpeg's stdout.
type FrameReader struct {
	cmd    *exec.Cmd
	out    io.ReadCloser
	size   int
	errBuf *bytes.Buffer
}

func NewFrameReader(input, vf string, w, h int, pixfmt string) (*FrameReader, error) {
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-i", input, "-an", "-sn", "-dn"}
	if vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-f", "rawvideo", "-pix_fmt", pixfmt, "pipe:1")
	cmd := exec.Command(FFmpeg(), args...)
	errBuf := &bytes.Buffer{}
	cmd.Stderr = errBuf
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &FrameReader{cmd: cmd, out: out, size: w * h * BytesPer(pixfmt), errBuf: errBuf}, nil
}

func (r *FrameReader) FrameSize() int { return r.size }

// Next fills buf (must be FrameSize long). ok=false means clean EOF.
func (r *FrameReader) Next(buf []byte) (bool, error) {
	n, err := io.ReadFull(r.out, buf[:r.size])
	if err == io.EOF {
		return false, nil
	}
	if err == io.ErrUnexpectedEOF {
		return false, fmt.Errorf("ffmpeg: truncated frame (%d/%d bytes): %s", n, r.size, strings.TrimSpace(r.errBuf.String()))
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *FrameReader) Close() error {
	r.out.Close()
	if err := r.cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg decode: %v: %s", err, strings.TrimSpace(r.errBuf.String()))
	}
	return nil
}

// Encoder is an ffmpeg process whose stdin accepts raw frames.
type Encoder struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	errBuf *bytes.Buffer
}

func NewEncoder(args []string) (*Encoder, error) {
	cmd := exec.Command(FFmpeg(), args...)
	errBuf := &bytes.Buffer{}
	cmd.Stderr = errBuf
	cmd.Stdout = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	return &Encoder{cmd: cmd, in: in, errBuf: errBuf}, nil
}

func (e *Encoder) Write(p []byte) (int, error) { return e.in.Write(p) }

func (e *Encoder) Close() error {
	if err := e.in.Close(); err != nil {
		return err
	}
	if err := e.cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg encode: %v: %s", err, strings.TrimSpace(e.errBuf.String()))
	}
	return nil
}

// Run executes ffmpeg to completion, capturing stderr in error messages.
func Run(args ...string) error {
	cmd := exec.Command(FFmpeg(), args...)
	errBuf := &bytes.Buffer{}
	cmd.Stderr = errBuf
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return nil
}

// SceneCuts returns timestamps (seconds) where the scene score exceeds thr.
func SceneCuts(input string, threshold float64) ([]float64, error) {
	vf := fmt.Sprintf("select='gt(scene,%g)',metadata=print:file=-", threshold)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-i", input, "-an", "-sn", "-dn", "-vf", vf, "-f", "null", "-"}
	cmd := exec.Command(FFmpeg(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg scene: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var cuts []float64
	lastPTS := -1.0
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lavfi.scene_score=") {
			if lastPTS >= 0 {
				cuts = append(cuts, lastPTS)
			}
			continue
		}
		if i := strings.Index(line, "pts_time:"); i >= 0 {
			fields := strings.Fields(line[i+len("pts_time:"):])
			if len(fields) > 0 {
				if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
					lastPTS = v
				}
			}
		}
	}
	return cuts, nil
}
