package probe

import (
	"fmt"
	"os"

	"github.com/abema/go-mp4"
)

// DurationSeconds 读取 mp4/mov 的真实播放时长（秒，向上取整到秒）。
func DurationSeconds(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	info, err := mp4.Probe(f)
	if err != nil {
		return 0, fmt.Errorf("not a valid mp4/mov: %w", err)
	}
	if info.Timescale == 0 || info.Duration == 0 {
		return 0, fmt.Errorf("missing duration in moov/mvhd")
	}
	sec := info.Duration / uint64(info.Timescale)
	if info.Duration%uint64(info.Timescale) != 0 {
		sec++
	}
	if sec <= 0 {
		return 0, fmt.Errorf("zero duration")
	}
	return int64(sec), nil
}
