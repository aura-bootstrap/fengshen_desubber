package ffx

import (
	"reflect"
	"testing"
)

func TestColorEncodeArgs(t *testing.T) {
	mi := &MediaInfo{
		ColorRange:     "tv",
		ColorSpace:     "bt709",
		ColorTransfer:  "bt709",
		ColorPrimaries: "bt709",
	}
	want := []string{
		"-color_range", "tv",
		"-colorspace", "bt709",
		"-color_trc", "bt709",
		"-color_primaries", "bt709",
	}
	if got := mi.ColorEncodeArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestColorEncodeArgsSkipsUnknown(t *testing.T) {
	mi := &MediaInfo{ColorSpace: "bt2020nc", ColorTransfer: "unknown", ColorRange: ""}
	want := []string{"-colorspace", "bt2020nc"}
	if got := mi.ColorEncodeArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	var empty MediaInfo
	if got := empty.ColorEncodeArgs(); got != nil {
		t.Errorf("empty MediaInfo produced args: %v", got)
	}
}

func TestEncodeProfileSDR(t *testing.T) {
	mi := &MediaInfo{PixFmt: "yuv420p", BitDepth: 8, ColorSpace: "bt709"}
	codec, pixFmt, extra := mi.EncodeProfile()
	if codec != "libx264" || pixFmt != "yuv420p" || extra != nil {
		t.Errorf("SDR profile = %s/%s %v, want libx264/yuv420p nil", codec, pixFmt, extra)
	}
	if mi.IsHDR() {
		t.Error("SDR bt709 flagged HDR")
	}
}

func TestEncodeProfileHDR10(t *testing.T) {
	mi := &MediaInfo{
		PixFmt:         "yuv420p10le",
		BitDepth:       10,
		ColorSpace:     "bt2020nc",
		ColorTransfer:  "smpte2084",
		ColorPrimaries: "bt2020",
		MasterDisplay: &MasteringDisplay{
			Rx: 34000, Ry: 16000, Gx: 13250, Gy: 34500,
			Bx: 7500, By: 3000, Wx: 15635, Wy: 16450,
			MaxLum: 10000000, MinLum: 50,
		},
		MaxCLL: 1000, MaxFALL: 400,
	}
	codec, pixFmt, extra := mi.EncodeProfile()
	if codec != "libx265" || pixFmt != "yuv420p10le" {
		t.Errorf("HDR profile = %s/%s, want libx265/yuv420p10le", codec, pixFmt)
	}
	wantParams := "-x265-params"
	wantValue := "master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,50):max-cll=1000,400"
	found := false
	for i, a := range extra {
		if a == wantParams && i+1 < len(extra) && extra[i+1] == wantValue {
			found = true
		}
	}
	if !found {
		t.Errorf("missing x265 mastering params in %v", extra)
	}
	if !mi.IsHDR() {
		t.Error("HDR10 source not flagged HDR")
	}
}

func TestEncodeProfileCLLOnly(t *testing.T) {
	mi := &MediaInfo{BitDepth: 10, ColorTransfer: "smpte2084", MaxCLL: 2000, MaxFALL: 700}
	_, _, extra := mi.EncodeProfile()
	found := false
	for i, a := range extra {
		if a == "-x265-params" && i+1 < len(extra) && extra[i+1] == "max-cll=2000,700" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing max-cll params in %v", extra)
	}
}

func TestProbeBitDepth(t *testing.T) {
	cases := []struct {
		bitsRaw, pixFmt string
		want            int
	}{
		{"10", "yuv420p10le", 10},
		{"", "yuv420p10le", 10},
		{"", "yuv420p", 8},
		{"0", "yuv420p", 8},
		{"", "rgb24", 8},
		{"", "yuv444p12le", 12},
	}
	for _, c := range cases {
		if got := probeBitDepth(c.bitsRaw, c.pixFmt); got != c.want {
			t.Errorf("probeBitDepth(%q,%q)=%d, want %d", c.bitsRaw, c.pixFmt, got, c.want)
		}
	}
}

func TestRatNum(t *testing.T) {
	if got := ratNum("34000/50000", 50000); got != 34000 {
		t.Errorf("ratNum chroma = %d, want 34000", got)
	}
	if got := ratNum("10000000/10000", 10000); got != 10000000 {
		t.Errorf("ratNum lum = %d, want 10000000", got)
	}
	if got := ratNum("", 50000); got != 0 {
		t.Errorf("ratNum empty = %d, want 0", got)
	}
}
