// Package downloader 并发下载 TS 段 + AES-128-CBC 解密 + ffmpeg 封装 MP4。
// 密钥还原对应浏览器版 transformKey：key[i] = rawKey[i] ^ companion[i % len]，
// companion 为 m3u8 同名 .jpg 文件（base64 文本）。
package downloader

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apj9ehckiw/mediapulse/backend/internal/ffmpeg"
	"github.com/apj9ehckiw/mediapulse/backend/internal/site"
)

// Progress 下载进度回调参数。
type Progress struct {
	Done      int
	Total     int
	Bytes     int64  // 累计已下载字节（解密前，用于速度估算）
	SpeedBps  float64 // 瞬时速率（字节/秒，滑动采样估算；0 = 暂无采样）
}

// Options 下载选项。
type Options struct {
	Workers    int           // 并发段数
	OnProgress func(Progress)
}

// Downloader 视频下载器。
type Downloader struct {
	Client  *site.Client
	Workers int
}

// New 创建下载器。
func New(c *site.Client, workers int) *Downloader {
	if workers <= 0 {
		workers = 8
	}
	return &Downloader{Client: c, Workers: workers}
}

// resolveKey 下载 enc key 与 companion，XOR 还原并用首段验证 TS 同步字节。
func (d *Downloader) resolveKey(pl *site.Playlist, m3u8URL string) ([]byte, error) {
	rawKey, err := d.Client.FetchBytes(pl.KeyURL)
	if err != nil {
		return nil, fmt.Errorf("fetch key: %w", err)
	}
	firstSeg, err := d.Client.FetchBytes(firstSegmentURL(m3u8URL, pl.Segments[0]))
	if err != nil {
		return nil, fmt.Errorf("fetch first segment: %w", err)
	}

	// companion 候选：xxx.jpg、xxx_i.jpg，最后兜底空 companion（原始 key）
	var candidates [][]byte
	for _, suffix := range []string{".jpg", "_i.jpg"} {
		compURL := strings.Replace(m3u8URL, ".m3u8", suffix, 1)
		if data, err := d.Client.FetchBytes(compURL); err == nil {
			if comp, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data))); err == nil && len(comp) > 0 {
				candidates = append(candidates, comp)
			}
		}
	}
	candidates = append(candidates, nil)

	iv, err := hexIV(pl.IVHex)
	if err != nil {
		return nil, err
	}
	trimmed := trimAESBlock(firstSeg)

	for _, comp := range candidates {
		key := rawKey
		if comp != nil {
			key = xorKey(rawKey, comp)
		}
		if validTS(key, iv, trimmed) {
			return key, nil
		}
	}
	return nil, errors.New("key resolve failed: no companion candidate decrypts to valid TS")
}

func xorKey(raw, comp []byte) []byte {
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = b ^ comp[i%len(comp)]
	}
	return out
}

func hexIV(s string) ([]byte, error) {
	if len(s) != 32 {
		return nil, fmt.Errorf("bad IV %q", s)
	}
	return hexDecode(s)
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd hex length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexNibble(s[2*i])
		lo, ok2 := hexNibble(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, errors.New("bad hex")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func trimAESBlock(data []byte) []byte { return data[:len(data)/16*16] }

// validTS 解密首段并检查 MPEG-TS 同步字节 0x47（每 188 字节一个）。
func validTS(key, iv, seg []byte) bool {
	if len(seg) < 188*4 {
		return false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return false
	}
	dec := make([]byte, len(seg))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(dec, seg)
	packets := len(dec) / 188
	if packets > 10 {
		packets = 10
	}
	ok := 0
	for i := 0; i < packets; i++ {
		if dec[i*188] == 0x47 {
			ok++
		}
	}
	return packets > 0 && ok >= packets*8/10
}

func firstSegmentURL(m3u8URL, seg string) string {
	base := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]
	return base + seg
}

// Download 下载完整 m3u8 视频，解密后按序写 TS，再 ffmpeg 封装为 MP4。
func (d *Downloader) Download(m3u8URL, outMP4 string, opt Options) error {
	pl, err := d.Client.ParsePlaylist(m3u8URL)
	if err != nil {
		return err
	}
	if opt.Workers > 0 {
		d.Workers = opt.Workers
	}

	key, err := d.resolveKey(pl, m3u8URL)
	if err != nil {
		return err
	}
	iv, err := hexIV(pl.IVHex)
	if err != nil {
		return err
	}

	base := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]
	total := len(pl.Segments)
	buf := make([][]byte, total)
	var done atomic.Int64
	var bytesDL atomic.Int64
	var firstErr error
	var errMu sync.Mutex
	sem := make(chan struct{}, d.Workers)
	var wg sync.WaitGroup

	start := time.Now()
	// 段下载速度采样（供进度回调估算瞬时速率）：最近窗口的 (时间, 字节) 快照
	var sampleT atomic.Int64 // unix 毫秒
	var sampleB atomic.Int64
	sampleT.Store(start.UnixMilli())

	for i, name := range pl.Segments {
		wg.Add(1)
		go func(idx int, segName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			data, err := d.Client.FetchBytes(base + segName)
			if err == nil {
				data, err = decryptSegment(data, key, iv)
			}
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("segment %d: %w", idx, err)
				}
				errMu.Unlock()
				return
			}
			buf[idx] = data
			n := done.Add(1)
			b := bytesDL.Add(int64(len(data)))
			if opt.OnProgress != nil && (n%50 == 0 || n == int64(total)) {
				// 用上一个采样点到现在的字节差估算瞬时速率
				prevT, prevB := sampleT.Load(), sampleB.Load()
				nowT := time.Now().UnixMilli()
				dt := float64(nowT-prevT) / 1000
				speed := 0.0
				if dt > 0.3 {
					speed = float64(b-prevB) / dt
					sampleT.Store(nowT)
					sampleB.Store(b)
				}
				opt.OnProgress(Progress{Done: int(n), Total: total, Bytes: b, SpeedBps: speed})
			}
		}(i, name)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	elapsed := time.Since(start)
	_ = elapsed
	_ = bytesDL

	// 按序写入临时 TS
	tmpTS := outMP4 + ".ts.tmp"
	if err := writeTS(tmpTS, buf); err != nil {
		return err
	}

	if err := remuxMP4(tmpTS, outMP4); err != nil {
		os.Remove(tmpTS)
		return err
	}
	os.Remove(tmpTS)
	return nil
}

func decryptSegment(data, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	n := len(data) / 16 * 16
	if n == 0 {
		return data, nil
	}
	dec := make([]byte, n)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(dec, data[:n])
	return append(dec, data[n:]...), nil
}

func writeTS(path string, buf [][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, seg := range buf {
		if _, err := f.Write(seg); err != nil {
			return err
		}
	}
	return nil
}

// remuxMP4 ffmpeg 无损封装 TS -> MP4（+faststart 把 moov 移到文件头，
// 浏览器无需 Range 跳到文件尾即可出首帧/缩略图）。
func remuxMP4(tsPath, mp4Path string) error {
	ff := ffmpeg.Path()
	if ff == "" {
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			ff = p
		} else {
			return errors.New("ffmpeg 未就绪（自动安装中或失败，查看运行日志/设置页）")
		}
	}
	var stderr bytes.Buffer
	cmd := exec.Command(ff, "-y", "-loglevel", "error", "-i", tsPath,
		"-c", "copy", "-movflags", "+faststart", mp4Path)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, truncate(stderr.String(), 300))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SafeFilename 清理文件名非法字符。
func SafeFilename(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|', ' ', '\t', '\n':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	s = strings.Trim(b.String(), "_")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// FileExists 报告文件是否存在且非空。
func FileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}
