// Package ffmpeg 自动检测 ffmpeg；缺失时按平台/架构下载并安装到数据目录 bin/ 下。
// 安装源：
//   - Windows amd64/arm64: BtbN FFmpeg-Builds（GitHub，zip）
//   - Linux amd64/arm64/arm: johnvansickle static builds（tar.xz）
//   - macOS amd64: evermeet.cx（zip）；darwin/arm64 无稳定直链，提示手动安装
package ffmpeg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

const binName = "ffmpeg"

// State 安装器当前状态：ready | installing | failed | missing
var (
	mu      sync.Mutex
	state   = "missing"
	binPath string // 已解析的可执行文件绝对路径
	lastErr string
)

// Path 返回已解析的 ffmpeg 路径；未就绪返回空串（调用方回退到 PATH 查找）。
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return binPath
}

// Status 返回状态与最近一次错误。
func Status() (st, errMsg string) {
	mu.Lock()
	defer mu.Unlock()
	return state, lastErr
}

// Detect 仅检测（PATH 与 binDir），不下载。返回 ffmpeg 可执行路径。
func Detect(binDir string) (string, bool) {
	if p, err := exec.LookPath(binName); err == nil {
		return p, true
	}
	local := filepath.Join(binDir, binName+exeExt())
	if fi, err := os.Stat(local); err == nil && fi.Mode().IsRegular() && fi.Size() > 1<<20 {
		return local, true
	}
	return "", false
}

// Ensure 检测 ffmpeg，缺失时自动下载安装到 binDir。完成后状态置 ready/failed。
func Ensure(binDir string) error {
	mu.Lock()
	state = "checking"
	mu.Unlock()

	if p, ok := Detect(binDir); ok {
		mu.Lock()
		binPath, state, lastErr = p, "ready", ""
		mu.Unlock()
		return nil
	}

	mu.Lock()
	state = "installing"
	mu.Unlock()
	if err := install(binDir); err != nil {
		mu.Lock()
		state, lastErr = "failed", err.Error()
		mu.Unlock()
		return err
	}
	mu.Lock()
	binPath, state, lastErr = filepath.Join(binDir, binName+exeExt()), "ready", ""
	mu.Unlock()
	return nil
}

// Install 强制（重新）安装，供网页端手动触发。
func Install(binDir string) error {
	mu.Lock()
	if state == "installing" {
		mu.Unlock()
		return errors.New("正在安装中")
	}
	state = "installing"
	mu.Unlock()
	if err := install(binDir); err != nil {
		mu.Lock()
		state, lastErr = "failed", err.Error()
		mu.Unlock()
		return err
	}
	mu.Lock()
	binPath, state, lastErr = filepath.Join(binDir, binName+exeExt()), "ready", ""
	mu.Unlock()
	return nil
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// downloadURL 按 GOOS/GOARCH 返回压缩包直链与格式（zip | tar.xz | tar.gz）。
func downloadURL() (string, string, error) {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip", "zip", nil
		case "arm64":
			return "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-winarm64-gpl.zip", "zip", nil
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz", "tar.xz", nil
		case "arm64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz", "tar.xz", nil
		case "arm":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-armhf-static.tar.xz", "tar.xz", nil
		}
	case "darwin":
		if runtime.GOARCH == "amd64" {
			return "https://evermeet.cx/ffmpeg/get/zip", "zip", nil
		}
	}
	return "", "", fmt.Errorf("暂不支持 %s/%s 的自动下载，请手动安装 ffmpeg", runtime.GOOS, runtime.GOARCH)
}

func install(binDir string) error {
	url, format, err := downloadURL()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	data, err := download(url)
	if err != nil {
		return err
	}
	var bin []byte
	switch format {
	case "zip":
		bin, err = extractFromZip(data, binName+exeExt())
	case "tar.xz":
		bin, err = extractFromTarXZ(data, binName)
	case "tar.gz":
		bin, err = extractFromTarGZ(data, binName)
	}
	if err != nil {
		return err
	}
	out := filepath.Join(binDir, binName+exeExt())
	if err := os.WriteFile(out, bin, 0o755); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(out, 0o755)
	}
	return nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Minute}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				return data, nil
			}
			lastErr = err
		} else {
			if resp != nil {
				resp.Body.Close()
			}
			lastErr = fmt.Errorf("http %s: %v", resp.Status, err)
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
	}
	return nil, fmt.Errorf("下载失败: %w", lastErr)
}

// matchEntry 判断压缩包条目是否为目标文件（根目录或任意子目录下名为 name 的文件）。
func matchEntry(entry, name string) bool {
	return filepath.ToSlash(entry) == name || strings.HasSuffix(filepath.ToSlash(entry), "/"+name)
}

// extractFromZip 从 zip 中提取名为 name 的第一个条目。
func extractFromZip(data []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if matchEntry(f.Name, name) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("压缩包中未找到 %s", name)
}

// extractFromTarXZ 从 tar.xz 中提取路径以 /<name> 结尾的第一个条目。
func extractFromTarXZ(data []byte, name string) ([]byte, error) {
	xr, err := xz.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return extractFromTar(xr, name)
}

// extractFromTarGZ 从 tar.gz 中提取路径以 /<name> 结尾的第一个条目。
func extractFromTarGZ(data []byte, name string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return extractFromTar(gr, name)
}

func extractFromTar(r io.Reader, name string) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && matchEntry(hdr.Name, name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("压缩包中未找到 %s", name)
}
