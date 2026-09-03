// Package config 应用配置：持久化到 config.json，网页端可改。
package config

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuthorConfig 单个被监控作者。
type AuthorConfig struct {
	UID      int64  `json:"uid"`      // 作者 UID（/homepage/<uid>）
	Note     string `json:"note"`     // 备注（可选，展示用）
	Enabled  bool   `json:"enabled"`  // 是否启用监控
}

// Config 全局配置。
type Config struct {
	APIBase      string         `json:"apiBase"`
	Authors      []AuthorConfig `json:"authors"`
	Interval     int            `json:"intervalSec"`    // 轮询间隔秒，0=不自动
	ListType     int            `json:"listType"`       // 0=全部 1=最新 3=精华
	Workers      int            `json:"workers"`        // 段下载并发
	AutoDownload bool           `json:"autoDownload"`   // false=仅发现记录，需手动下载（默认）
	// 自动下载的发布时间下限：仅自动下载发布时间在该日期 00:00 之后的帖子。
	// 空 = 不限制（所有新发现的视频都自动下载）。
	// 手动「发现」页下载不受此限制。
	AutoDownloadAfter string `json:"autoDownloadAfter,omitempty"`
	// GitHub 加速代理前缀：仅作用于 ffmpeg 安装包下载（Windows/macOS 的下载源在 GitHub，
	// 国内直连慢）。如 https://ghproxy.net/ 或 https://mirror.ghproxy.com/ ；
	// 用法为 <代理前缀>https://github.com/... （前缀拼接原始 URL）。
	// 空 = 直连 GitHub。Linux 的 johnvansickle.com 源不走代理。
	GitHubProxy        string `json:"githubProxy,omitempty"`
	// ffmpeg 自动下载开关：默认 false = 缺失时仅提示用户，由用户在设置页手动触发安装。
	FFmpegAutoInstall bool   `json:"ffmpegAutoInstall,omitempty"`
	Password          string `json:"password,omitempty"`     // 访问密码；空 = 无密码
	AuthDisabled      bool   `json:"authDisabled,omitempty"` // true = 显式停用鉴权（首次部署的设置密码向导不再出现）
}

// Store 配置存储（线程安全，内存持有 + 文件持久化）。
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// Default 默认配置。
func Default() Config {
	return Config{
		APIBase:  "https://j194de04581960.xyz",
		Authors:  []AuthorConfig{{UID: 168672751201, Note: "", Enabled: true}},
		Interval: 600,
		ListType: 1,
		Workers:  8,
	}
}

// Open 读取配置文件；不存在则用 def 落盘初始值。
func Open(path string, def Config) (*Store, error) {
	s := &Store{path: path, cfg: def}
	data, err := os.ReadFile(path)
	if err == nil {
		var c Config
		if err := json.Unmarshal(data, &c); err == nil && c.APIBase != "" {
			// 兼容缺省字段
			if c.Interval < 0 {
				c.Interval = 0
			}
			if c.Workers <= 0 {
				c.Workers = 8
			}
			if c.ListType != 0 && c.ListType != 1 && c.ListType != 3 {
				c.ListType = 1
			}
			s.cfg = c
			return s, nil
		}
	}
	// 文件不存在或损坏：落盘初始值
	if err := s.save(); err != nil {
		return nil, err
	}
	return s, nil
}

// Get 当前配置（深拷贝）。
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clone()
}

func (s *Store) clone() Config {
	c := s.cfg
	c.Authors = make([]AuthorConfig, len(s.cfg.Authors))
	copy(c.Authors, s.cfg.Authors)
	return c
}

// Update 覆盖保存配置（校验后落盘）。
func (s *Store) Update(c Config) (Config, error) {
	if err := validate(&c); err != nil {
		return s.Get(), err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
	return s.clone(), s.save()
}

func validate(c *Config) error {
	if c.APIBase == "" {
		c.APIBase = Default().APIBase
	}
	for i := len(c.APIBase) - 1; i >= 0; i-- {
		if c.APIBase[i] == '/' {
			c.APIBase = c.APIBase[:i]
		} else {
			break
		}
	}
	if c.Workers <= 0 {
		c.Workers = 8
	}
	if c.Workers > 32 {
		c.Workers = 32
	}
	if c.Interval < 0 {
		c.Interval = 0
	}
	if c.Interval > 0 && c.Interval < 60 {
		c.Interval = 60 // 最低 1 分钟，避免打爆站点
	}
	if c.ListType != 0 && c.ListType != 1 && c.ListType != 3 {
		c.ListType = 1
	}
	// GitHub 加速代理前缀：须以 / 结尾的 URL（http/https），拼接在原始 GitHub 直链前；
	// 非法输入清空 = 直连
	if p := strings.TrimSpace(c.GitHubProxy); p != "" {
		u, err := url.Parse(p)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			c.GitHubProxy = ""
		} else if !strings.HasSuffix(c.GitHubProxy, "/") {
			c.GitHubProxy = c.GitHubProxy + "/"
		}
	}
	// 自动下载发布时间下限：仅接受 YYYY-MM-DD；非法/零值清空 = 不限制
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(c.AutoDownloadAfter)); err != nil || t.IsZero() {
		c.AutoDownloadAfter = ""
	} else {
		c.AutoDownloadAfter = t.Format("2006-01-02")
	}
	// 作者去重 + 清理（允许为空：全部删除后仅手动检查，由「作者」页重新添加）
	seen := map[int64]bool{}
	out := make([]AuthorConfig, 0, len(c.Authors))
	for _, a := range c.Authors {
		if a.UID <= 0 || seen[a.UID] {
			continue
		}
		seen[a.UID] = true
		out = append(out, a)
	}
	c.Authors = out
	return nil
}

// save 落盘（调用方需持锁）。
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
