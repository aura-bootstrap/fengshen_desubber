package probe

// httpReadSeeker:在支持 Range 的 HTTP 资源上实现 io.ReadSeeker。
// 每次 Read/Seek 不缓存全文件,按窗口惰性回源(go-mp4 Probe 只读 ftyp/moov,
// 实际传输量远小于文件大小)。窗口外的 Read 重新发 Range 请求。

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const rangeWindow = 4 << 20 // 每次 Range 回源 4MB

type httpReadSeeker struct {
	url    string
	hc     *http.Client
	size   int64
	off    int64 // 当前逻辑读位置
	buf    []byte
	bufOff int64 // buf 起点在资源中的偏移
}

func newHTTPReadSeeker(url string) (*httpReadSeeker, error) {
	rs := &httpReadSeeker{url: url, hc: &http.Client{Timeout: 60 * time.Second}, bufOff: -1}
	// 预签名 URL 签名绑定 HTTP 方法,HEAD 会验签失败;用 bytes=0-0 Range GET
	// 探总大小(Content-Range 携带)与 Range 支持,首个字节顺带落入缓冲区。
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := rs.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("range probe http %d(remote must support range)", resp.StatusCode)
	}
	// Content-Range: bytes 0-0/<total>
	cr := resp.Header.Get("Content-Range")
	idx := strings.LastIndex(cr, "/")
	if idx < 0 {
		return nil, fmt.Errorf("bad content-range: %s", cr)
	}
	rs.size, err = strconv.ParseInt(cr[idx+1:], 10, 64)
	if err != nil || rs.size <= 0 {
		return nil, fmt.Errorf("bad content-range total: %s", cr)
	}
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	rs.buf, rs.bufOff = buf, 0
	return rs, nil
}

func (r *httpReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.off + offset
	case io.SeekEnd:
		abs = r.size + offset
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative position")
	}
	r.off = abs
	return abs, nil
}

func (r *httpReadSeeker) Read(p []byte) (int, error) {
	if r.off >= r.size {
		return 0, io.EOF
	}
	// buf 命中窗口则直接服务;否则按当前位置重新回源一个窗口。
	if r.bufOff < 0 || r.off < r.bufOff || r.off >= r.bufOff+int64(len(r.buf)) {
		if err := r.fill(); err != nil {
			return 0, err
		}
	}
	n := copy(p, r.buf[r.off-r.bufOff:])
	r.off += int64(n)
	return n, nil
}

func (r *httpReadSeeker) fill() error {
	end := r.off + rangeWindow - 1
	if end >= r.size {
		end = r.size - 1
	}
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.off, end))
	resp, err := r.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("range get http %d", resp.StatusCode)
	}
	// 校验服务端确实按所请区间回(200 全量时只取需要的前缀)。
	if cr := resp.Header.Get("Content-Range"); resp.StatusCode == http.StatusPartialContent && cr != "" {
		if !strings.HasPrefix(cr, "bytes "+strconv.FormatInt(r.off, 10)+"-") {
			return fmt.Errorf("unexpected content-range: %s", cr)
		}
	}
	buf, err := io.ReadAll(io.LimitReader(resp.Body, rangeWindow))
	if err != nil {
		return err
	}
	r.buf, r.bufOff = buf, r.off
	if len(buf) == 0 {
		return io.EOF
	}
	return nil
}
