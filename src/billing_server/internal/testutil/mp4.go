package testutil

import (
	"os"
	"testing"

	"github.com/abema/go-mp4"
)

// WriteTestMP4 生成仅含 ftyp+moov/mvhd 的最小 mp4，时长 durationSec 秒。
func WriteTestMP4(t *testing.T, path string, durationSec int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := mp4.NewWriter(f)
	if _, err := w.StartBox(&mp4.BoxInfo{Type: mp4.BoxTypeFtyp()}); err != nil {
		t.Fatal(err)
	}
	if _, err := mp4.Marshal(w, &mp4.Ftyp{
		MajorBrand:   mp4.BrandISOM(),
		MinorVersion: 1,
		CompatibleBrands: []mp4.CompatibleBrandElem{
			{CompatibleBrand: mp4.BrandISOM()},
		},
	}, mp4.Context{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.EndBox(); err != nil {
		t.Fatal(err)
	}

	if _, err := w.StartBox(&mp4.BoxInfo{Type: mp4.BoxTypeMoov()}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.StartBox(&mp4.BoxInfo{Type: mp4.BoxTypeMvhd()}); err != nil {
		t.Fatal(err)
	}
	if _, err := mp4.Marshal(w, &mp4.Mvhd{
		Timescale:   1000,
		DurationV0:  uint32(durationSec * 1000),
		Rate:        0x00010000,
		Volume:      0x0100,
		Matrix:      [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
		NextTrackID: 2,
	}, mp4.Context{}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.EndBox(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.EndBox(); err != nil {
		t.Fatal(err)
	}
}

// WriteBadFile 写一个非 mp4 文件。
func WriteBadFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("not an mp4 at all"), 0o644); err != nil {
		t.Fatal(err)
	}
}
