package probe

import (
	"fmt"
	"io"
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
	return probeDuration(f)
}

// DurationSecondsURL 经 HTTP Range 请求读取远端 mp4/mov 的真实播放时长（秒），
// 不整文件下载（moov 在尾部时只回源尾部片段）。rs 须支持 Range。
func DurationSecondsURL(url string) (int64, error) {
	rs, err := newHTTPReadSeeker(url)
	if err != nil {
		return 0, err
	}
	return probeDuration(rs)
}

func probeDuration(rs io.ReadSeeker) (int64, error) {
	info, err := mp4.Probe(rs)
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
