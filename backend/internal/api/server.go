// Package api HTTP 服务：REST API + SSE + 静态前端。
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apj9ehckiw/mediapulse/backend/internal/config"
	"github.com/apj9ehckiw/mediapulse/backend/internal/ffmpeg"
	"github.com/apj9ehckiw/mediapulse/backend/internal/history"
	"github.com/apj9ehckiw/mediapulse/backend/internal/monitor"
	"github.com/apj9ehckiw/mediapulse/backend/web"
)

// Server HTTP 服务。
type Server struct {
	mon      *monitor.Monitor
	store    *config.Store
	hist     *history.Store
	addr     string
	dataDir  string
	sseMu    sync.Mutex
	sseChans map[chan monitor.Event]struct{}

	sessMu      sync.Mutex
	sessions    map[string]time.Time // 会话 token -> 过期时间
	sessFile    string               // 会话持久化文件（重启后登录态保留）
	sessDirty   bool                 // 有变更待落盘
	sessStop    chan struct{}
}

const (
	sessionCookie = "hj_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// New 创建服务。
func New(mon *monitor.Monitor, store *config.Store, hist *history.Store, addr, dataDir string) *Server {
	s := &Server{
		mon: mon, store: store, hist: hist, addr: addr, dataDir: dataDir,
		sseChans: map[chan monitor.Event]struct{}{},
		sessions: map[string]time.Time{},
		sessFile: filepath.Join(dataDir, "sessions.json"),
		sessStop: make(chan struct{}),
	}
	s.loadSessions()
	go s.persistSessions()
	mon.OnEvent(s.broadcast)
	return s
}

func (s *Server) broadcast(ev monitor.Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseChans {
		select {
		case ch <- ev:
		default: // 慢消费者丢弃
		}
	}
}

// Handler 路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/check", s.handleCheck)
	mux.HandleFunc("GET /api/videos", s.handleVideos)
	mux.HandleFunc("GET /api/videos/file/", s.handleVideoFile)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handleUpdateConfig)
	mux.HandleFunc("GET /api/downloads", s.handleDownloads)
	mux.HandleFunc("POST /api/downloads/clear", s.handleDownloadsClear)
	mux.HandleFunc("POST /api/downloads/delete", s.handleDownloadsDelete)
	mux.HandleFunc("GET /api/discovered", s.handleDiscovered)
	mux.HandleFunc("POST /api/download", s.handleDownload)
	mux.HandleFunc("POST /api/task/cancel", s.handleTaskCancel)
	mux.HandleFunc("POST /api/discovered/dismiss", s.handleDismissDiscovered)
	mux.HandleFunc("POST /api/authors/add", s.handleAddAuthor)
	mux.HandleFunc("POST /api/authors/enable", s.handleAuthorEnable)
	mux.HandleFunc("POST /api/authors/remove", s.handleAuthorRemove)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/state", s.handleAuthState)
	mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("GET /api/ffmpeg", s.handleFFmpegStatus)
	mux.HandleFunc("POST /api/ffmpeg/install", s.handleFFmpegInstall)

	// 嵌入式前端
	if dist, err := fs.Sub(web.Dist, "dist"); err == nil {
		fileServer := http.FileServer(http.FS(dist))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// SPA fallback：非 /api 且文件不存在时回 index.html
			if r.URL.Path != "/" {
				if _, err := fs.Stat(dist, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
					r.URL.Path = "/"
				}
			}
			fileServer.ServeHTTP(w, r)
		})
	}
	return s.withAuth(mux)
}

// authMode 当前鉴权状态：open=未启用 login=需登录 setup=首次部署待设置密码
func (s *Server) authMode() string {
	cfg := s.store.Get()
	switch {
	case cfg.Password != "":
		return "login"
	case !cfg.AuthDisabled:
		return "setup"
	default:
		return "open"
	}
}

// withAuth 鉴权中间件：静态前端始终放行（登录/初始设置页由前端展示）。
// 已设密码 → /api/* 需会话；首次部署（未设密码且未停用鉴权）→ 仅放行初始化接口，强制先设置密码。
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") ||
			p == "/api/auth/login" || p == "/api/auth/state" || p == "/api/auth/setup" {
			next.ServeHTTP(w, r)
			return
		}
		mode := s.authMode()
		if mode == "open" {
			next.ServeHTTP(w, r)
			return
		}
		if mode == "login" {
			if ck, err := r.Cookie(sessionCookie); err == nil && s.validSession(ck.Value) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// setup 待完成：除初始化接口外全部拦截
		http.Error(w, "setup required", http.StatusForbidden)
	})
}

func (s *Server) validSession(token string) bool {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	exp, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, token)
		s.sessDirty = true
		return false
	}
	return true
}

func (s *Server) newSession() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(buf)
	s.sessMu.Lock()
	now := time.Now()
	for t, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, t)
		}
	}
	s.sessions[token] = now.Add(sessionTTL)
	s.sessDirty = true
	s.sessMu.Unlock()
	return token
}

// dropSession 删除单个会话（登出）。
func (s *Server) dropSession(token string) {
	s.sessMu.Lock()
	if _, ok := s.sessions[token]; ok {
		delete(s.sessions, token)
		s.sessDirty = true
	}
	s.sessMu.Unlock()
}

// dropAllSessions 清空全部会话（密码变更后作废旧登录态）。
func (s *Server) dropAllSessions() {
	s.sessMu.Lock()
	s.sessions = map[string]time.Time{}
	s.sessDirty = true
	s.sessMu.Unlock()
}

// ==========================================
// 会话持久化：sessions.json，服务重启后登录态保留（30 天 TTL 不变）
// ==========================================

// loadSessions 启动时加载持久化会话，过滤已过期的。
func (s *Server) loadSessions() {
	data, err := os.ReadFile(s.sessFile)
	if err != nil {
		return
	}
	var saved map[string]time.Time
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	now := time.Now()
	s.sessMu.Lock()
	for tok, exp := range saved {
		if now.Before(exp) {
			s.sessions[tok] = exp
		}
	}
	s.sessMu.Unlock()
}

// persistSessions 后台定期落盘（有变更时），进程退出由 stopSessions 收尾。
func (s *Server) persistSessions() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.sessStop:
			s.saveSessions()
			return
		case <-ticker.C:
			s.saveSessions()
		}
	}
}

// stopSessions 停止后台落盘并做最后保存（main 优雅退出时调用）。
func (s *Server) stopSessions() {
	select {
	case <-s.sessStop:
	default:
		close(s.sessStop)
	}
	// 给 persistSessions 一点时间完成最后一次落盘
	time.Sleep(100 * time.Millisecond)
	s.saveSessions()
}

// saveSessions 落盘（无变更时跳过）。
func (s *Server) saveSessions() {
	s.sessMu.Lock()
	if !s.sessDirty {
		s.sessMu.Unlock()
		return
	}
	data, err := json.Marshal(s.sessions)
	s.sessDirty = false
	s.sessMu.Unlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.sessFile), 0o755)
	if err := os.WriteFile(s.sessFile, data, 0o600); err != nil {
		log.Printf("[session] persist failed: %v", err)
		// 失败时恢复脏标记，下轮重试
		s.sessMu.Lock()
		s.sessDirty = true
		s.sessMu.Unlock()
	}
}

// handleAuthState 返回鉴权状态（前端据此展示 初始设置/登录/主界面）。
// mode: open=免鉴权 / login=需登录 / authed=已设密码且当前会话有效 / setup=首次部署待设置密码。
func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	mode := s.authMode()
	if mode == "login" {
		// 已设密码：检查当前请求的会话 Cookie，有效则直接进入主界面（刷新免登录）
		if ck, err := r.Cookie(sessionCookie); err == nil && s.validSession(ck.Value) {
			mode = "authed"
		}
	}
	writeJSON(w, map[string]any{"mode": mode})
}

// handleAuthSetup 首次部署设置访问密码（仅在密码未设置且未停用鉴权时允许），完成后自动登录。
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	cfg := s.store.Get()
	if cfg.Password != "" {
		http.Error(w, "密码已设置，请直接登录", http.StatusForbidden)
		return
	}
	if cfg.AuthDisabled {
		http.Error(w, "鉴权已停用，如需启用请在设置页配置密码", http.StatusForbidden)
		return
	}
	pw := strings.TrimSpace(req.Password)
	if len(pw) < 4 {
		http.Error(w, "密码至少 4 位", http.StatusBadRequest)
		return
	}
	c := cfg
	c.Password = pw
	c.AuthDisabled = false
	if _, err := s.store.Update(c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.dropAllSessions()
	s.mon.EmitInfo("首次部署：访问密码已设置，鉴权已启用")
	// 设置完成后直接签发会话，免二次登录
	token := s.newSession()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, map[string]any{"ok": true})
}

// handleLogin 密码登录，签发会话 Cookie。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	pw := s.store.Get().Password
	if pw == "" {
		if s.authMode() == "setup" {
			http.Error(w, "请先完成初始密码设置", http.StatusForbidden)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(pw)) != 1 {
		time.Sleep(800 * time.Millisecond) // 减缓暴力尝试
		http.Error(w, "密码错误", http.StatusUnauthorized)
		return
	}
	token := s.newSession()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	writeJSON(w, map[string]any{"ok": true})
}

// handleLogout 注销会话。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if ck, err := r.Cookie(sessionCookie); err == nil {
		s.dropSession(ck.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]any{"ok": true})
}

// handleFFmpegStatus ffmpeg 检测/安装状态（含安装包下载进度）。
func (s *Server) handleFFmpegStatus(w http.ResponseWriter, r *http.Request) {
	st, errMsg := ffmpeg.Status()
	done, total := ffmpeg.Progress()
	writeJSON(w, map[string]any{
		"state": st, "path": ffmpeg.Path(), "error": errMsg,
		"goos": runtime.GOOS, "goarch": runtime.GOARCH,
		"progressDone": done, "progressTotal": total, // -1 = 无进度信息
	})
}

// handleFFmpegInstall 手动触发 ffmpeg 检测/下载安装（后台执行）。
// 下载进度通过事件流推送（网页端日志与设置页可见）。
func (s *Server) handleFFmpegInstall(w http.ResponseWriter, r *http.Request) {
	binDir := filepath.Join(s.dataDir, "bin")
	// 应用当前配置的 GitHub 加速代理（设置页可改，安装前生效）
	cfg := s.store.Get()
	ffmpeg.SetGitHubProxy(cfg.GitHubProxy)
	// 注册进度回调 -> 事件流（约每 2MB 一次，避免刷屏）
	lastMB := int64(-1)
	ffmpeg.SetProgressFn(func(done, total int64) {
		mb := done / 1048576
		if total > 0 {
			if mb/8 != lastMB || done == total {
				lastMB = mb / 8
				s.mon.EmitInfo("ffmpeg 下载中: %s / %s（%.0f%%）",
					humanBytes(done), humanBytes(total), float64(done)/float64(total)*100)
			}
		} else if mb/32 != lastMB {
			lastMB = mb / 32
			s.mon.EmitInfo("ffmpeg 下载中: %s（大小未知）", humanBytes(done))
		}
	})
	go func() {
		defer ffmpeg.SetProgressFn(nil)
		s.mon.EmitInfo("开始下载安装 ffmpeg（平台 %s/%s）", runtime.GOOS, runtime.GOARCH)
		if err := ffmpeg.Install(binDir); err != nil {
			s.mon.EmitInfo("ffmpeg 安装失败: %v", err)
			return
		}
		s.mon.EmitInfo("ffmpeg 已就绪: %s", ffmpeg.Path())
	}()
	writeJSON(w, map[string]any{"ok": true})
}

// humanBytes 字节数人性化显示。
func humanBytes(n int64) string {
	const unit = 1048576
	if n >= unit {
		return fmt.Sprintf("%.1f MB", float64(n)/float64(unit))
	}
	return fmt.Sprintf("%.0f KB", float64(n)/1024)
}

// ListenAndServe 启动。
func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.addr, s.Handler())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mon.Snapshot())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Accept") == "text/event-stream" {
		s.handleSSE(w, r)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.mon.Events(limit))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan monitor.Event, 64)
	s.sseMu.Lock()
	s.sseChans[ch] = struct{}{}
	s.sseMu.Unlock()
	defer func() {
		s.sseMu.Lock()
		delete(s.sseChans, ch)
		s.sseMu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	// 回放最近 50 条
	for _, ev := range s.mon.Events(50) {
		writeSSE(w, ev)
	}
	fl.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			writeSSE(w, ev)
			fl.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev monitor.Event) {
	data, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// handleTopics 拉作者帖子列表（只读，不触发下载）。
func (s *Server) handleTopics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"error": "use /api/check to refresh; topics come from monitor state"})
}

// handleCheck 手动触发检查。
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	s.mon.TriggerCheck()
	writeJSON(w, map[string]any{"ok": true})
}

// handleGetConfig 读取配置（密码不回传，仅返回 hasPassword）。
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.Get()
	has := cfg.Password != ""
	cfg.Password = ""
	writeJSON(w, map[string]any{
		"apiBase":             cfg.APIBase,
		"authors":             cfg.Authors,
		"intervalSec":         cfg.Interval,
		"listType":            cfg.ListType,
		"workers":             cfg.Workers,
		"autoDownload":        cfg.AutoDownload,
		"autoDownloadAfter":   cfg.AutoDownloadAfter,
		"githubProxy":         cfg.GitHubProxy,
		"ffmpegAutoInstall":   cfg.FFmpegAutoInstall,
		"hasPassword":         has,
		"authDisabled":        cfg.AuthDisabled,
	})
}

// handleUpdateConfig 更新配置（网页端设置页）。
// 密码规则：请求里 password 非空 = 设置新密码（至少 4 位）；为空且 clearPassword=false = 保持不变；clearPassword=true = 停用鉴权。
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		config.Config
		ClearPassword bool `json:"clearPassword"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	c := req.Config
	old := s.store.Get()
	switch {
	case req.ClearPassword:
		c.Password = ""
		c.AuthDisabled = true
	case strings.TrimSpace(c.Password) != "":
		if len(strings.TrimSpace(c.Password)) < 4 {
			http.Error(w, "密码至少 4 位", http.StatusBadRequest)
			return
		}
		c.Password = strings.TrimSpace(c.Password)
		c.AuthDisabled = false
	default:
		c.Password = old.Password
		c.AuthDisabled = old.AuthDisabled
	}
	// 防 GBK/ANSI 乱写：客户端按系统 ANSI 编码（如 Windows curl 直接传中文）提交时，
	// 字节流不是合法 UTF-8，JSON 解码会产出 U+FFFD 替换符。检测到则拒绝保存，
	// 避免把配置文件里的作者昵称等中文字段永久损坏。
	for _, a := range c.Authors {
		if strings.ContainsRune(a.Note, 0xFFFD) {
			http.Error(w, "作者昵称包含乱码（非 UTF-8 输入），请通过网页端修改", http.StatusBadRequest)
			return
		}
	}
	if strings.ContainsRune(c.AutoDownloadAfter, 0xFFFD) || strings.ContainsRune(c.APIBase, 0xFFFD) || strings.ContainsRune(c.Password, 0xFFFD) || strings.ContainsRune(c.GitHubProxy, 0xFFFD) {
		http.Error(w, "配置字段包含乱码（非 UTF-8 输入），请通过网页端修改", http.StatusBadRequest)
		return
	}
	// 请求未带 authors（旧客户端/部分提交）时保留现有作者列表
	if len(c.Authors) == 0 && len(old.Authors) > 0 {
		c.Authors = old.Authors
	}
	updated, err := s.store.Update(c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// ffmpeg 自动安装开关即时生效（下次检测/启动时应用）
	ffmpeg.SetAutoInstall(updated.FFmpegAutoInstall)
	ffmpeg.SetGitHubProxy(updated.GitHubProxy)
	// 密码变更后作废所有旧会话
	if updated.Password != old.Password {
		s.dropAllSessions()
	}
	// 配置生效：触发一次检查（新作者立即被扫描）
	s.mon.TriggerCheck()
	has := updated.Password != ""
	authOff := updated.AuthDisabled
	updated.Password = ""
	writeJSON(w, map[string]any{
		"apiBase":           updated.APIBase,
		"authors":           updated.Authors,
		"intervalSec":       updated.Interval,
		"listType":          updated.ListType,
		"workers":           updated.Workers,
		"autoDownload":      updated.AutoDownload,
		"autoDownloadAfter": updated.AutoDownloadAfter,
		"githubProxy":       updated.GitHubProxy,
		"ffmpegAutoInstall": updated.FFmpegAutoInstall,
		"hasPassword":       has,
		"authDisabled":      authOff,
	})
}

// handleDownloads 下载记录列表（最新在前），附作者名字（配置备注，缺失时回退 UID）。
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows := s.hist.List(limit)
	authors := s.store.Get().Authors
	nameOf := func(uid int64) string {
		for _, a := range authors {
			if a.UID == uid && a.Note != "" {
				return a.Note
			}
		}
		return ""
	}
	out := make([]downloadView, len(rows))
	for i, rec := range rows {
		name := nameOf(rec.AuthorUID)
		if name == "" {
			name = strconv.FormatInt(rec.AuthorUID, 10)
		}
		out[i] = downloadView{Record: rec, AuthorName: name}
	}
	writeJSON(w, out)
}

// downloadView 下载记录 + 展示用作者名字（不落盘）。
type downloadView struct {
	history.Record
	AuthorName string `json:"authorName"`
}

// handleDownloadsClear 清理下载记录（status: all|done|failed|skipped），
// 并同步清除对应帖子的下载去重记录（帖子之后可重新被发现/下载）。
func (s *Server) handleDownloadsClear(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Status {
	case "", "all", "done", "failed", "skipped":
	default:
		http.Error(w, "status must be all|done|failed|skipped", http.StatusBadRequest)
		return
	}
	removedRows := s.hist.Clear(req.Status)
	ids := make([]int64, 0, len(removedRows))
	seen := map[int64]bool{}
	for _, rec := range removedRows {
		if !seen[rec.TopicID] {
			seen[rec.TopicID] = true
			ids = append(ids, rec.TopicID)
		}
	}
	dedup := s.mon.ClearDownloaded(ids)
	s.mon.EmitInfo("已清理下载记录 %d 条（%s），同步清除去重 %d 条", len(removedRows), displayStatus(req.Status), dedup)
	writeJSON(w, map[string]any{"ok": true, "removed": len(removedRows), "dedup": dedup})
}

func displayStatus(s string) string {
	switch s {
	case "done":
		return "成功"
	case "failed":
		return "失败"
	case "skipped":
		return "跳过"
	default:
		return "全部"
	}
}

// handleDownloadsDelete 删除单条下载记录；成功记录同步清除该帖子的去重条目。
func (s *Server) handleDownloadsDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TopicID int64  `json:"topicId"`
		Status  string `json:"status"`
		At      string `json:"at"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch req.Status {
	case "done", "failed", "skipped":
	default:
		http.Error(w, "status must be done|failed|skipped", http.StatusBadRequest)
		return
	}
	rec, ok := s.hist.Delete(req.TopicID, req.Status, req.At)
	if !ok {
		http.Error(w, "record not found", http.StatusNotFound)
		return
	}
	dedup := 0
	if rec.Status == "done" {
		dedup = s.mon.ClearDownloaded([]int64{rec.TopicID})
	}
	s.mon.EmitInfo("已删除下载记录：帖子 %d（%s）", rec.TopicID, displayStatus(rec.Status))
	writeJSON(w, map[string]any{"ok": true, "dedup": dedup})
}

// handleDiscovered 发现待下载列表（自动下载关闭时积累）。
func (s *Server) handleDiscovered(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mon.Discovered())
}

// handleDownload 手动下载选中的发现记录。
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TopicIDs []int64 `json:"topicIds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.TopicIDs) == 0 {
		http.Error(w, "topicIds is required", http.StatusBadRequest)
		return
	}
	if len(req.TopicIDs) > 500 {
		http.Error(w, "too many topicIds", http.StatusBadRequest)
		return
	}
	enqueued := s.mon.EnqueueManual(req.TopicIDs)
	writeJSON(w, map[string]any{"ok": true, "enqueued": enqueued})
}

// handleTaskCancel 取消下载任务（pending/resolving/downloading 可取消）。
func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TopicID int64 `json:"topicId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TopicID <= 0 {
		http.Error(w, "topicId is required", http.StatusBadRequest)
		return
	}
	if s.mon.CancelTask(req.TopicID) {
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	http.Error(w, "任务不存在或当前状态不可取消", http.StatusConflict)
}

// handleDismissDiscovered 忽略选中的发现记录。
func (s *Server) handleDismissDiscovered(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TopicIDs []int64 `json:"topicIds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.mon.DismissDiscovered(req.TopicIDs)
	writeJSON(w, map[string]any{"ok": true})
}

// handleAddAuthor 添加作者（名字由站点自动获取）。
func (s *Server) handleAddAuthor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID int64 `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	a, err := s.mon.AddAuthor(req.UID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "uid": a.UID, "name": a.Note, "enabled": a.Enabled})
}

// handleAuthorEnable 启用/停用作者。
func (s *Server) handleAuthorEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID     int64 `json:"uid"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	a, err := s.mon.SetAuthorEnabled(req.UID, req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "uid": a.UID, "name": a.Note, "enabled": a.Enabled})
}

// handleAuthorRemove 删除作者。
func (s *Server) handleAuthorRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID int64 `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.mon.RemoveAuthor(req.UID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type videoInfo struct {
	TopicID    int64   `json:"topicId"`
	AuthorUID  int64   `json:"authorUid,omitempty"`
	AuthorName string  `json:"authorName,omitempty"` // 展示用作者昵称（配置备注，缺省 UID）
	Title      string  `json:"title"`
	CreateTime string  `json:"createTime"`
	File       string  `json:"file"`
	SizeMB     float64 `json:"sizeMB"`
	DoneAt     string  `json:"doneAt"`
}

func (s *Server) handleVideos(w http.ResponseWriter, r *http.Request) {
	snap := s.mon.Snapshot()
	// 作者展示名：优先配置备注（下载记录里的作者可能已被删除，回退 UID）
	nameOf := func(uid int64) string {
		for _, a := range snap.Authors {
			if a.UID == uid && a.Note != "" {
				return a.Note
			}
		}
		return strconv.FormatInt(uid, 10)
	}
	var list []videoInfo
	for id, rec := range snap.Records {
		vi := videoInfo{
			TopicID: id, AuthorUID: rec.AuthorUID, Title: rec.Title, CreateTime: rec.CreateTime,
			File: rec.File, DoneAt: rec.DoneAt,
		}
		if rec.AuthorUID == 0 {
			vi.AuthorUID = 0 // 旧记录无作者：不显示作者名
		} else {
			vi.AuthorName = nameOf(rec.AuthorUID)
		}
		if fi, err := os.Stat(filepath.Join(snap.OutDir, rec.File)); err == nil {
			vi.SizeMB = float64(fi.Size()) / 1048576
		}
		list = append(list, vi)
	}
	if list == nil {
		list = []videoInfo{}
	}
	// createTime 降序
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].CreateTime > list[j-1].CreateTime; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	writeJSON(w, list)
}

func (s *Server) handleVideoFile(w http.ResponseWriter, r *http.Request) {
	// 允许一层作者子目录（videos/<作者名>/<文件>.mp4），拒绝穿越出 videos/
	name := strings.TrimPrefix(r.URL.Path, "/api/videos/file/")
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		http.Error(w, "bad filename", http.StatusBadRequest)
		return
	}
	snap := s.mon.Snapshot()
	path := filepath.Join(snap.OutDir, filepath.FromSlash(name))
	absPath, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, "bad filename", http.StatusBadRequest)
		return
	}
	absOut, err := filepath.Abs(snap.OutDir)
	if err != nil || !strings.HasPrefix(absPath, absOut+string(filepath.Separator)) {
		http.Error(w, "bad filename", http.StatusBadRequest)
		return
	}
	fi, err := os.Stat(absPath)
	if err != nil || fi.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	// 支持 Range（浏览器拖动进度条）
	http.ServeFile(w, r, absPath)
}
