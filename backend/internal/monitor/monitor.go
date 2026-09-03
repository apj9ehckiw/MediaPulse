// Package monitor 监控调度：轮询多个作者帖子列表，增量下载新视频。
package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apj9ehckiw/mediapulse/backend/internal/config"
	"github.com/apj9ehckiw/mediapulse/backend/internal/downloader"
	"github.com/apj9ehckiw/mediapulse/backend/internal/history"
	"github.com/apj9ehckiw/mediapulse/backend/internal/site"
)

// Status 任务状态。
type Status string

const (
	StatusPending     Status = "pending"
	StatusResolving   Status = "resolving"
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusFailed      Status = "failed"
	StatusSkipped     Status = "skipped"
	StatusCanceled    Status = "canceled" // 用户手动取消（可重新下载：发现页重新触发）
)

// Task 一次下载任务。
type Task struct {
	TopicID    int64     `json:"topicId"`
	AuthorUID  int64     `json:"authorUid"`
	AuthorName string    `json:"authorName,omitempty"` // 展示用作者昵称（配置备注，缺省 UID）
	Title      string    `json:"title"`
	CreateTime string    `json:"createTime"`
	Status     Status    `json:"status"`
	Progress   float64   `json:"progress"` // 0-100
	SegDone    int       `json:"segDone"`
	SegTotal   int       `json:"segTotal"`
	SpeedBps   float64   `json:"speedBps,omitempty"` // 下载瞬时速率（字节/秒，仅 downloading 状态）
	BytesDone  int64     `json:"bytesDone,omitempty"` // 已下载字节（累计）
	File       string    `json:"file,omitempty"`
	Error      string    `json:"error,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

// Event 事件流条目。
type Event struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info|ok|warn|error|dim
	Msg   string    `json:"msg"`
}

// DownloadedRecord 已下载记录（state.json）。
type DownloadedRecord struct {
	AuthorUID  int64  `json:"authorUid,omitempty"`
	Title      string `json:"title"`
	CreateTime string `json:"createTime"`
	File       string `json:"file"`
	DoneAt     string `json:"done_at"`
}

// DiscoveredRecord 发现待下载记录（自动下载关闭时，检查到的新视频）。
type DiscoveredRecord struct {
	TopicID      int64  `json:"topicId"`
	AuthorUID    int64  `json:"authorUid"`
	Title        string `json:"title"`
	CreateTime   string `json:"createTime"`
	DiscoveredAt string `json:"discoveredAt"`
	Verified     bool   `json:"verified,omitempty"` // 已通过帖子详情核验确实带视频
}

type persistedState struct {
	Topics     map[int64]DownloadedRecord `json:"topics"`
	Discovered map[int64]DiscoveredRecord `json:"discovered,omitempty"`
}

// Snapshot 当前状态快照（API 用）。
type Snapshot struct {
	APIBase         string                     `json:"apiBase"`
	OutDir          string                     `json:"outDir"`
	Interval        int                        `json:"intervalSec"`
	ListType        int                        `json:"listType"`
	Workers         int                        `json:"workers"`
	AutoDownload    bool                       `json:"autoDownload"`
	AuthEnabled     bool                       `json:"authEnabled"`
	Running         bool                       `json:"running"`
	Checking        bool                       `json:"checking"`
	CheckDone       int                        `json:"checkDone"`
	CheckTotal      int                        `json:"checkTotal"`
	Downloaded      int                        `json:"downloaded"`
	DiscoveredCount int                        `json:"discoveredCount"`
	TotalMB         float64                    `json:"totalMB"`
	Tasks           []*Task                    `json:"tasks"`
	Records         map[int64]DownloadedRecord `json:"records"`
	Authors         []AuthorStat               `json:"authors"`
}

// AuthorStat 单作者统计。
type AuthorStat struct {
	UID        int64  `json:"uid"`
	Note       string `json:"note"`
	Enabled    bool   `json:"enabled"`
	Downloaded int    `json:"downloaded"`
	Videos     int    `json:"videos"`  // 检测到的视频帖总数（含已下载与待处理）
	Pending    int    `json:"pending"` // 待处理（发现未下载，含检查失败可重试的）
	LastCheck  string `json:"lastCheck,omitempty"`
}

// Paths 监控器的文件路径。
type Paths struct {
	OutDir    string
	StateFile string
}

// Monitor 监控器（多作者）。
type Monitor struct {
	paths Paths
	store *config.Store

	mu       sync.Mutex
	client   *site.Client
	dl       *downloader.Downloader
	state    persistedState
	queue    []*Task
	tasks    map[int64]*Task
	events   []Event
	running  bool
	checking bool
	checkDone  int // 本轮检查已完成的作者数（供网页端进度显示）
	checkTotal int // 本轮检查的启用作者总数
	wake     chan struct{}
	dlWake   chan struct{}
	stopCh   chan struct{}
	subs     []func(Event)
	lastCheck map[int64]string
	// 任务取消信号：topicID -> cancel 函数（下载中/解析中的任务）
	cancels map[int64]context.CancelFunc

	hist *history.Store
}

// New 创建监控器。
func New(store *config.Store, paths Paths, hist *history.Store) *Monitor {
	m := &Monitor{
		paths:     paths,
		store:     store,
		client:    site.New(store.Get().APIBase),
		state:     persistedState{Topics: map[int64]DownloadedRecord{}, Discovered: map[int64]DiscoveredRecord{}},
		tasks:     map[int64]*Task{},
		wake:      make(chan struct{}, 1),
		dlWake:    make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		lastCheck: map[int64]string{},
		cancels:   map[int64]context.CancelFunc{},
		hist:      hist,
	}
	cfg := store.Get()
	m.dl = downloader.New(m.client, cfg.Workers)
	// 数据目录 bin/ 下的 ffmpeg（remux 兜底查找用）
	downloader.SetBinDir(filepath.Dir(paths.StateFile))
	m.loadState()
	return m
}

// Run 启动监控循环（含启动即检查一次）与下载队列。
func (m *Monitor) Run() {
	m.migrateToAuthorFolders()
	m.running = true
	go func() {
		m.checkAll()
		ticker := time.NewTicker(pollInterval(m.store))
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.checkAll()
			case <-m.wake:
				m.checkAll()
			}
		}
	}()
	// 下载队列：checkAll 与手动下载统一经此执行
	go func() {
		for {
			select {
			case <-m.stopCh:
				return
			case <-m.dlWake:
				m.drainQueue()
			}
		}
	}()
}

// OnEvent 注册事件订阅者（Web SSE）。
func (m *Monitor) OnEvent(fn func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs = append(m.subs, fn)
}

func (m *Monitor) emit(level, format string, args ...any) {
	ev := Event{Time: time.Now(), Level: level, Msg: fmt.Sprintf(format, args...)}
	m.mu.Lock()
	m.events = append(m.events, ev)
	if len(m.events) > 500 {
		m.events = m.events[len(m.events)-500:]
	}
	subs := append([]func(Event){}, m.subs...)
	m.mu.Unlock()
	for _, fn := range subs {
		func() {
			defer func() { _ = recover() }()
			fn(ev)
		}()
	}
	log.Printf("[%s] %s", level, ev.Msg)
}

func (m *Monitor) loadState() {
	data, err := os.ReadFile(m.paths.StateFile)
	if err != nil {
		return
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err == nil && st.Topics != nil {
		if st.Discovered == nil {
			st.Discovered = map[int64]DiscoveredRecord{}
		}
		m.state = st
	}
}

func (m *Monitor) saveState() {
	data, err := json.MarshalIndent(m.state, "", " ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(m.paths.StateFile), 0o755)
	_ = os.WriteFile(m.paths.StateFile, data, 0o644)
}

// Snapshot 生成快照。
func (m *Monitor) Snapshot() Snapshot {
	cfg := m.store.Get()
	m.mu.Lock()
	defer m.mu.Unlock()
	tasks := make([]*Task, len(m.queue))
	copy(tasks, m.queue)
	for i, j := 0, len(tasks)-1; i < j; i, j = i+1, j-1 {
		tasks[i], tasks[j] = tasks[j], tasks[i]
	}
	// 任务展示名：优先配置备注（锁内用已取出的 cfg 映射，避免嵌套加锁）
	nameOf := func(uid int64) string {
		for _, a := range cfg.Authors {
			if a.UID == uid && a.Note != "" {
				return a.Note
			}
		}
		return strconv.FormatInt(uid, 10)
	}
	for _, t := range tasks {
		t.AuthorName = nameOf(t.AuthorUID)
	}
	var totalMB float64
	counts := map[int64]int{}
	pendingCounts := map[int64]int{}
	videoSeen := map[int64]int64{} // topicID -> authorUID（并集，防双计）
	for tid, rec := range m.state.Topics {
		uid := rec.AuthorUID
		if uid == 0 {
			uid = legacyUID() // 兼容旧 state.json 无 authorUid 的记录
		}
		counts[uid]++
		videoSeen[tid] = uid
		if fi, err := os.Stat(filepath.Join(m.paths.OutDir, rec.File)); err == nil {
			totalMB += float64(fi.Size()) / 1048576
		}
	}
	for tid, rec := range m.state.Discovered {
		if _, done := m.state.Topics[tid]; done {
			continue
		}
		pendingCounts[rec.AuthorUID]++
		if _, ok := videoSeen[tid]; !ok {
			videoSeen[tid] = rec.AuthorUID
		}
	}
	videoCounts := map[int64]int{}
	pendingTotal := 0
	for _, uid := range videoSeen {
		videoCounts[uid]++
	}
	for _, n := range pendingCounts {
		pendingTotal += n
	}
	authors := make([]AuthorStat, 0, len(cfg.Authors))
	for _, a := range cfg.Authors {
		authors = append(authors, AuthorStat{
			UID: a.UID, Note: a.Note, Enabled: a.Enabled,
			Downloaded: counts[a.UID],
			Videos:     videoCounts[a.UID],
			Pending:    pendingCounts[a.UID],
			LastCheck:  m.lastCheck[a.UID],
		})
	}
	return Snapshot{
		APIBase:         cfg.APIBase,
		OutDir:          m.paths.OutDir,
		Interval:        cfg.Interval,
		ListType:        cfg.ListType,
		Workers:         cfg.Workers,
		AutoDownload:    cfg.AutoDownload,
		AuthEnabled:     cfg.Password != "",
		Running:         m.running,
		Checking:        m.checking,
		CheckDone:       m.checkDone,
		CheckTotal:      m.checkTotal,
		Downloaded:      len(m.state.Topics),
		DiscoveredCount: pendingTotal,
		TotalMB:         totalMB,
		Tasks:           tasks,
		Records:         m.state.Topics,
		Authors:         authors,
	}
}

// legacyUID 返回旧版单作者默认 UID（历史记录兼容）。
func legacyUID() int64 { return 168672751201 }

// migrateToAuthorFolders 启动时把 videos/ 根目录的旧下载迁移到 <作者名>/ 子文件夹，
// 并更新 state.json 的文件路径；文件缺失（如被手动删除）的记录原样保留。
func (m *Monitor) migrateToAuthorFolders() {
	// 先取作者名映射（store 有自己的锁，避免在 monitor 锁内嵌套调用）
	folderOf := map[int64]string{}
	for _, a := range m.store.Get().Authors {
		folderOf[a.UID] = safeName(a.Note, 40)
	}

	m.mu.Lock()
	moved := 0
	for id, rec := range m.state.Topics {
		if strings.Contains(rec.File, "/") || strings.Contains(rec.File, "\\") {
			continue // 已是子路径
		}
		src := filepath.Join(m.paths.OutDir, rec.File)
		if _, err := os.Stat(src); err != nil {
			continue // 根目录没有这个文件，不迁移
		}
		uid := rec.AuthorUID
		if uid == 0 {
			uid = legacyUID()
		}
		folder, ok := folderOf[uid]
		if !ok || folder == "" {
			folder = strconv.FormatInt(uid, 10)
		}
		dstDir := filepath.Join(m.paths.OutDir, folder)
		dst := filepath.Join(dstDir, rec.File)
		if _, err := os.Stat(dst); err == nil {
			continue // 目标已存在，不覆盖
		}
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			continue
		}
		rec.File = folder + "/" + rec.File
		m.state.Topics[id] = rec
		moved++
	}
	if moved > 0 {
		m.saveState()
	}
	m.mu.Unlock()
	if moved > 0 {
		m.emit("info", "已把 %d 个旧视频整理到作者文件夹", moved)
	}
}

// Events 返回最近事件。
func (m *Monitor) Events(limit int) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	out := make([]Event, limit)
	copy(out, m.events[len(m.events)-limit:])
	return out
}

// TriggerCheck 手动触发一次检查（异步）。
func (m *Monitor) TriggerCheck() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// CheckAuthorAsync 增量检查单个作者（异步，不触发全局 checkAll）。
// 供 API 层在新增/启用单个作者时使用：只拉该作者的列表。
func (m *Monitor) CheckAuthorAsync(a config.AuthorConfig) {
	go m.checkAuthor(a)
}

// EmitInfo 写入一条 info 事件（供 API 层记录网页端操作）。
func (m *Monitor) EmitInfo(format string, args ...any) {
	m.emit("info", format, args...)
}

// pollInterval 读取轮询间隔（0 时退化为 60s 空转，仅响应手动触发）。
func pollInterval(store *config.Store) time.Duration {
	if n := store.Get().Interval; n > 0 {
		return time.Duration(n) * time.Second
	}
	return time.Hour
}

// Stop 停止监控。
func (m *Monitor) Stop() { close(m.stopCh) }

var filenameSan = regexp.MustCompile(`[\\/:*?"<>|\s]+`)

func safeName(s string, maxLen int) string {
	s = filenameSan.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen])
	}
	return s
}

// checkAll 逐个作者拉列表、筛新帖、逐个下载。
func (m *Monitor) checkAll() {
	m.mu.Lock()
	if m.checking {
		m.mu.Unlock()
		return
	}
	m.checking = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.checking = false
		m.mu.Unlock()
	}()

	cfg := m.store.Get()
	// 应用配置：基址/并发可能被网页端改过
	if m.client.Base != cfg.APIBase {
		m.mu.Lock()
		m.client = site.New(cfg.APIBase)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.dl = downloader.New(m.client, cfg.Workers)
	m.mu.Unlock()

	// 只检查启用的作者；记录进度供网页端显示
	var targets []config.AuthorConfig
	for _, a := range cfg.Authors {
		if a.Enabled {
			targets = append(targets, a)
		}
	}
	m.mu.Lock()
	m.checkTotal = len(targets)
	m.checkDone = 0
	m.mu.Unlock()

	for _, a := range targets {
		m.checkAuthor(a)
		m.mu.Lock()
		m.checkDone++
		m.mu.Unlock()
	}
	if len(targets) == 0 {
		m.emit("warn", "没有启用的作者，请在左侧「作者」页添加并开启")
	}
}

// checkAuthor 检查单个作者。
func (m *Monitor) checkAuthor(a config.AuthorConfig) {
	name := m.authorName(a.UID)
	m.emit("info", "拉取作者 %s 帖子列表...", name)
	topics, err := m.client.ListTopics(a.UID, m.store.Get().ListType)
	if err != nil {
		m.emit("error", "作者 %s 列表拉取失败: %v", name, err)
		return
	}
	for i := range topics {
		topics[i].AuthorUID = a.UID
	}
	m.mu.Lock()
	m.lastCheck[a.UID] = time.Now().Format("2006-01-02 15:04:05")
	m.mu.Unlock()
	// 备注为空时用列表自带的 nickname 自动补全（不覆盖用户设置的备注）
	if nick := authorNickname(topics); nick != "" {
		m.ensureAuthorNote(a.UID, nick)
	}
	m.emit("info", "作者 %s 列表共 %d 帖", name, len(topics))

	auto := m.store.Get().AutoDownload
	// 自动下载发布时间下限："2006-01-02"；空 = 不限制
	autoAfter := ""
	if auto {
		autoAfter = strings.TrimSpace(m.store.Get().AutoDownloadAfter)
	}
	// pubTimeOK 帖子发布时间是否满足自动下载条件（无限制或晚于下限）
	pubTimeOK := func(createTime string) bool {
		if autoAfter == "" {
			return true
		}
		t, err := time.ParseInLocation("2006-01-02", autoAfter, time.Local)
		if err != nil {
			return true
		}
		// 帖子发布时间 "2026-05-20 09:42:51"（站点本地时区）
		ct, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(createTime), time.Local)
		if err != nil {
			// 时间缺失/解析失败：保守放行，交给后续流程
			return true
		}
		return ct.After(t)
	}

	// 先在锁内筛出待处理对象，再在锁外核验真实视频附件
	// （列表 hasVideo 标记不可靠：部分帖子视频附件 remoteUrl 为空）
	m.mu.Lock()
	var newCandidates []site.Topic
	var recheck []int64 // 旧版登记、未经详情核验的发现记录
	skippedOld := 0     // 自动下载时间条件不满足、跳过的帖子数
	for _, t := range topics {
		if !t.HasVideo {
			continue
		}
		if _, done := m.state.Topics[t.TopicID]; done {
			continue
		}
		if rec, exist := m.state.Discovered[t.TopicID]; exist {
			if !rec.Verified {
				recheck = append(recheck, t.TopicID)
			}
			continue
		}
		// 自动下载开启且设置了发布时间下限：不满足的帖子仍登记到「发现」页（可手动下载），不自动下载
		if auto && autoAfter != "" && !pubTimeOK(t.CreateTime) {
			m.state.Discovered[t.TopicID] = DiscoveredRecord{
				TopicID:      t.TopicID,
				AuthorUID:    a.UID,
				Title:        t.Title,
				CreateTime:   t.CreateTime,
				DiscoveredAt: time.Now().Format("2006-01-02 15:04:05"),
				Verified:     false, // 待核验：下一轮检查复核真实视频附件后再确认
			}
			skippedOld++
			continue
		}
		newCandidates = append(newCandidates, t)
	}
	needSave := skippedOld > 0
	m.mu.Unlock()

	var todo []site.Topic
	var verifiedNew []site.Topic
	noVideo, reVerified := 0, 0
	for _, t := range newCandidates {
		if m.topicHasVideo(t.TopicID) {
			verifiedNew = append(verifiedNew, t)
		} else {
			noVideo++
		}
		time.Sleep(300 * time.Millisecond)
	}
	// 下载顺序：发布时间升序（最旧先下）——与列表展示（最新在前）相反，
	// 用户看进度时视频按时间线从头追更
	site.SortByTimeAsc(verifiedNew)
	for _, id := range recheck {
		m.mu.Lock()
		rec, exist := m.state.Discovered[id]
		m.mu.Unlock()
		if !exist {
			continue
		}
		if !m.topicHasVideo(id) {
			m.mu.Lock()
			delete(m.state.Discovered, id)
			m.saveState()
			m.mu.Unlock()
			noVideo++
		} else {
			m.mu.Lock()
			rec.Verified = true
			m.state.Discovered[id] = rec
			m.saveState()
			m.mu.Unlock()
			reVerified++
		}
		time.Sleep(300 * time.Millisecond)
	}

	m.mu.Lock()
	for _, t := range verifiedNew {
		// 锁外核验期间状态可能变化，落库前再确认一次
		if _, done := m.state.Topics[t.TopicID]; done {
			continue
		}
		if _, exist := m.state.Discovered[t.TopicID]; exist {
			continue
		}
		if !auto {
			// 自动下载关闭：仅登记到发现列表，等待网页端手动选择下载
			m.state.Discovered[t.TopicID] = DiscoveredRecord{
				TopicID:      t.TopicID,
				AuthorUID:    t.AuthorUID,
				Title:        t.Title,
				CreateTime:   t.CreateTime,
				DiscoveredAt: time.Now().Format("2006-01-02 15:04:05"),
				Verified:     true,
			}
			continue
		}
		if _, queued := m.tasks[t.TopicID]; !queued {
			task := &Task{
				TopicID:    t.TopicID,
				AuthorUID:  a.UID,
				Title:      t.Title,
				CreateTime: t.CreateTime,
				Status:     StatusPending,
				AddedAt:    time.Now(),
			}
			m.tasks[t.TopicID] = task
			m.queue = append(m.queue, task)
		}
		todo = append(todo, t)
	}
	newDiscovered := len(verifiedNew)
	if newDiscovered > 0 || needSave {
		m.saveState()
	}
	m.mu.Unlock()
	if noVideo > 0 {
		m.emit("dim", "作者 %s 剔除 %d 个无视频帖子（详情核验）", name, noVideo)
	}
	if skippedOld > 0 {
		m.emit("dim", "作者 %s 有 %d 个帖子发布时间早于 %s，仅登记到「发现」页不自动下载", name, skippedOld, autoAfter)
	}

	if auto {
		if len(todo) == 0 {
			msg := "作者 %s 没有新视频（累计已下载 %d）"
			if skippedOld > 0 && newDiscovered == 0 {
				msg = "作者 %s 没有符合自动下载时间条件的新视频（累计已下载 %d）"
			}
			m.emit("info", msg, name, len(m.state.Topics))
			return
		}
		m.emit("ok", "作者 %s 发现 %d 个新视频，开始下载", name, len(todo))
		m.triggerDL()
		return
	}
	if newDiscovered > 0 {
		m.emit("ok", "作者 %s 发现 %d 个新视频（自动下载已关闭，请在「发现」页选择下载）", name, newDiscovered)
	} else {
		m.emit("info", "作者 %s 没有新视频", name)
	}
}

// topicHasVideo 拉取帖子详情确认真的带有视频附件。
// 详情拉取失败时保守返回 true，交给下载流程兜底判定。
func (m *Monitor) topicHasVideo(topicID int64) bool {
	m.mu.Lock()
	cl := m.client
	m.mu.Unlock()
	detail, err := cl.Detail(topicID)
	if err != nil {
		return true
	}
	return detail.VideoM3u8() != ""
}

func statusOf(err error) string {
	if err != nil && err.Error() == "no video attachment" {
		return "skipped"
	}
	return "failed"
}

// triggerDL 唤醒下载队列。
func (m *Monitor) triggerDL() {
	select {
	case m.dlWake <- struct{}{}:
	default:
	}
}

// dlConcurrency 同时下载的视频任务数（配置 workers 取半，最低 1 最高 4：
// 单视频内部已是段级高并发，多视频并发主要用于多作者追更时不再串行等待）
func (m *Monitor) dlConcurrency() int {
	w := m.store.Get().Workers
	n := w / 2
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

// drainQueue 从队列取 pending 任务，交给并发 worker 池执行。
// 每轮唤醒启动一批 worker，全部任务完成后返回（下轮唤醒再启动）。
func (m *Monitor) drainQueue() {
	workers := m.dlConcurrency()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-m.stopCh:
					return
				default:
				}
				m.mu.Lock()
				var t *Task
				for _, task := range m.queue {
					if task.Status == StatusPending {
						t = task
						break
					}
				}
				if t != nil {
					t.Status = StatusResolving // 立即占位，防止其他 worker 重复领取
				}
				m.mu.Unlock()
				if t == nil {
					return
				}
				if err := m.downloadOne(t); err != nil {
					// 取消的任务不写失败历史（downloadOne 已置 canceled 状态）
					if !errors.Is(err, errTaskCanceled) {
						m.addHistory(taskTopic(*t), "", 0, statusOf(err), err.Error())
						m.emit("error", "帖子 %d 失败: %v", t.TopicID, err)
					}
				}
				time.Sleep(300 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// errTaskCanceled 任务被用户取消（区分于失败：不写失败历史）。
var errTaskCanceled = errors.New("task canceled")

// CancelTask 取消任务：pending/resolving/downloading 状态可取消。
// 返回是否实际执行了取消。
func (m *Monitor) CancelTask(topicID int64) bool {
	m.mu.Lock()
	t, ok := m.tasks[topicID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	st := t.Status
	cancelFn := m.cancels[topicID]
	m.mu.Unlock()

	switch st {
	case StatusPending:
		// 未开始：直接移出队列
		m.mu.Lock()
		t.Status = StatusCanceled
		t.Error = ""
		t.SpeedBps = 0
		m.mu.Unlock()
		m.emit("dim", "帖子 %d 已取消（未开始下载）", topicID)
		return true
	case StatusResolving, StatusDownloading:
		// 进行中：触发 context 取消，downloadOne 会感知并置 canceled
		if cancelFn != nil {
			cancelFn()
			return true
		}
		return false
	}
	return false
}

func taskTopic(t Task) site.Topic {
	return site.Topic{TopicID: t.TopicID, AuthorUID: t.AuthorUID, Title: t.Title, CreateTime: t.CreateTime}
}

// isCanceled ctx 是否已被取消。
func isCanceled(ctx context.Context) bool {
	return ctx.Err() != nil
}

// cleanPartial 清理下载失败/取消的临时文件：.ts.tmp 与可能残留的半成品 mp4。
// mp4 仅在小于 1MB 时删除（正常完成的文件不会走到这里，保守起见只删明显残缺的）。
func cleanPartial(outPath string) {
	os.Remove(outPath + ".ts.tmp")
	if fi, err := os.Stat(outPath); err == nil && fi.Size() < 1<<20 {
		os.Remove(outPath)
	}
}

// taskCancelled 生成取消状态的任务更新函数。
func taskCancelled(topicID int64, ctx context.Context) func(*Task) {
	return func(t *Task) {
		t.Status = StatusCanceled
		t.Error = "已取消"
		t.SpeedBps = 0
	}
}

func authorNickname(topics []site.Topic) string {
	for _, t := range topics {
		if t.User.Nickname != "" {
			return t.User.Nickname
		}
	}
	return ""
}

// ensureAuthorNote 作者备注为空时自动填入站点昵称。
func (m *Monitor) ensureAuthorNote(uid int64, nick string) {
	// 乱码保护：昵称含 U+FFFD 时跳过，避免污染配置
	if strings.ContainsRune(nick, 0xFFFD) {
		return
	}
	cfg := m.store.Get()
	for i := range cfg.Authors {
		if cfg.Authors[i].UID == uid {
			if cfg.Authors[i].Note == "" {
				cfg.Authors[i].Note = nick
				if _, err := m.store.Update(cfg); err == nil {
					m.emit("info", "作者 %d 名字（自动获取）：%s", uid, nick)
				}
			}
			return
		}
	}
}

// authorName 下载命名用：优先配置备注，缺省用 UID 数字。
func (m *Monitor) authorName(uid int64) string {
	for _, a := range m.store.Get().Authors {
		if a.UID == uid && a.Note != "" {
			return a.Note
		}
	}
	return strconv.FormatInt(uid, 10)
}

// AddAuthor 自动获取名字并添加作者（幂等：已存在直接返回）。
func (m *Monitor) AddAuthor(uid int64) (config.AuthorConfig, error) {
	if uid <= 0 {
		return config.AuthorConfig{}, errors.New("UID 无效")
	}
	cfg := m.store.Get()
	for _, a := range cfg.Authors {
		if a.UID == uid {
			return a, nil
		}
	}
	m.mu.Lock()
	cl := m.client
	m.mu.Unlock()
	nick, total, err := cl.AuthorInfo(uid)
	if err != nil {
		return config.AuthorConfig{}, fmt.Errorf("获取作者信息失败: %w", err)
	}
	if nick == "" {
		return config.AuthorConfig{}, errors.New("无法获取作者名字：该 UID 没有可见帖子")
	}
	// 站点昵称损坏保护：响应被按非 UTF-8 解码时会出现 U+FFFD，拒绝落盘避免污染配置
	if strings.ContainsRune(nick, 0xFFFD) {
		return config.AuthorConfig{}, errors.New("获取到的作者名字包含乱码，请稍后重试")
	}
	cfg.Authors = append(cfg.Authors, config.AuthorConfig{UID: uid, Note: nick, Enabled: true})
	updated, err := m.store.Update(cfg)
	if err != nil {
		return config.AuthorConfig{}, err
	}
	m.emit("ok", "已添加作者 %s（共 %d 帖），开始增量检查（只检查该作者）", nick, total)
	// 增量：只检查新作者，不触发全局 checkAll（避免重新拉取全部已有作者的列表）
	go m.checkAuthor(config.AuthorConfig{UID: uid, Note: nick, Enabled: true})
	for _, a := range updated.Authors {
		if a.UID == uid {
			return a, nil
		}
	}
	return config.AuthorConfig{UID: uid, Note: nick, Enabled: true}, nil
}

// SetAuthorEnabled 启用/停用单个作者（网页端「作者」页开关）。
func (m *Monitor) SetAuthorEnabled(uid int64, enabled bool) (config.AuthorConfig, error) {
	cfg := m.store.Get()
	found := false
	for i := range cfg.Authors {
		if cfg.Authors[i].UID == uid {
			cfg.Authors[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return config.AuthorConfig{}, fmt.Errorf("作者 %d 不存在", uid)
	}
	updated, err := m.store.Update(cfg)
	if err != nil {
		return config.AuthorConfig{}, err
	}
	for _, a := range updated.Authors {
		if a.UID == uid {
			if enabled {
				m.emit("ok", "已启用作者 %s，增量检查该作者", m.authorName(uid))
				go m.checkAuthor(a)
			} else {
				m.emit("info", "已停用作者 %s", m.authorName(uid))
			}
			return a, nil
		}
	}
	return config.AuthorConfig{}, fmt.Errorf("作者 %d 不存在", uid)
}

// RemoveAuthor 删除作者（不删除已下载的视频与发现记录）。
func (m *Monitor) RemoveAuthor(uid int64) error {
	cfg := m.store.Get()
	out := cfg.Authors[:0]
	found := false
	for _, a := range cfg.Authors {
		if a.UID == uid {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found {
		return fmt.Errorf("作者 %d 不存在", uid)
	}
	cfg.Authors = out
	if _, err := m.store.Update(cfg); err != nil {
		return err
	}
	m.emit("info", "已删除作者 %s（已下载的视频保留在视频库）", m.authorName(uid))
	return nil
}

// ClearDownloaded 清除下载去重记录（state.json 的已下载帖子表），
// 被清除的帖子之后可重新被发现和下载；磁盘上的视频文件不受影响。
// 返回实际清除条数。
func (m *Monitor) ClearDownloaded(ids []int64) int {
	m.mu.Lock()
	removed := 0
	for _, id := range ids {
		if _, ok := m.state.Topics[id]; ok {
			delete(m.state.Topics, id)
			removed++
		}
	}
	if removed > 0 {
		m.saveState()
	}
	m.mu.Unlock()
	if removed > 0 {
		m.emit("info", "已清除 %d 条下载去重记录（帖子可重新下载，视频文件保留）", removed)
	}
	return removed
}

// EnqueueManual 将发现记录加入下载队列；返回实际入队数量。
// 已下载的跳过；排队/下载中的跳过；失败/跳过/已取消的重置为待下载（重试）。
// 入队顺序：按帖子发布时间升序（最旧先下，与自动下载一致）。
func (m *Monitor) EnqueueManual(ids []int64) int {
	m.mu.Lock()
	// 先按发布时间升序整理待入队列表
	type enqueueItem struct {
		id  int64
		rec DiscoveredRecord
	}
	var items []enqueueItem
	for _, id := range ids {
		if _, done := m.state.Topics[id]; done {
			continue
		}
		rec, ok := m.state.Discovered[id]
		if !ok {
			continue
		}
		items = append(items, enqueueItem{id: id, rec: rec})
	}
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].rec.CreateTime < items[j-1].rec.CreateTime; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}

	enqueued := 0
	for _, it := range items {
		id, rec := it.id, it.rec
		if t, queued := m.tasks[id]; queued {
			if t.Status == StatusPending || t.Status == StatusResolving || t.Status == StatusDownloading {
				continue
			}
			t.Status = StatusPending
			t.Error = ""
			t.Progress = 0
			t.SegDone, t.SegTotal = 0, 0
			t.FinishedAt = time.Time{}
			t.AddedAt = time.Now()
			enqueued++
			continue
		}
		task := &Task{
			TopicID:    rec.TopicID,
			AuthorUID:  rec.AuthorUID,
			Title:      rec.Title,
			CreateTime: rec.CreateTime,
			Status:     StatusPending,
			AddedAt:    time.Now(),
		}
		m.tasks[id] = task
		m.queue = append(m.queue, task)
		enqueued++
	}
	m.mu.Unlock()
	if enqueued > 0 {
		m.triggerDL()
	}
	return enqueued
}

// Discovered 返回未下载的发现记录（发布时间降序）。
func (m *Monitor) Discovered() []DiscoveredRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]DiscoveredRecord, 0, len(m.state.Discovered))
	for id, rec := range m.state.Discovered {
		if _, done := m.state.Topics[id]; done {
			continue
		}
		list = append(list, rec)
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].CreateTime > list[j-1].CreateTime; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list
}

// DismissDiscovered 忽略（移除）发现记录。
func (m *Monitor) DismissDiscovered(ids []int64) {
	m.mu.Lock()
	removed := 0
	for _, id := range ids {
		if _, ok := m.state.Discovered[id]; ok {
			delete(m.state.Discovered, id)
			removed++
		}
	}
	if removed > 0 {
		m.saveState()
	}
	m.mu.Unlock()
	if removed > 0 {
		m.emit("dim", "已忽略 %d 条发现记录", removed)
	}
}

func (m *Monitor) setTask(id int64, fn func(*Task)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		fn(t)
	}
}

func (m *Monitor) addHistory(t site.Topic, file string, sizeMB float64, status, errMsg string) {
	if m.hist == nil {
		return
	}
	m.hist.Add(history.Record{
		TopicID: t.TopicID, AuthorUID: t.AuthorUID, Title: t.Title,
		CreateTime: t.CreateTime, File: file, SizeMB: sizeMB,
		Status: status, Error: errMsg, At: time.Now(),
	})
}

func (m *Monitor) downloadOne(t *Task) error {
	topic := taskTopic(*t)
	// 取消信号：CancelTask 调用 cancelFn 后，各阶段感知退出
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[topic.TopicID] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, topic.TopicID)
		m.mu.Unlock()
	}()

	if err := ctx.Err(); err != nil {
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusCanceled })
		return errTaskCanceled
	}

	m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusResolving })

	detail, err := m.client.Detail(topic.TopicID)
	if err != nil {
		if isCanceled(ctx) {
			m.setTask(topic.TopicID, taskCancelled(topic.TopicID, ctx))
			return errTaskCanceled
		}
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusFailed; t.Error = err.Error(); t.SpeedBps = 0 })
		return fmt.Errorf("detail: %w", err)
	}
	preview := detail.VideoM3u8()
	if preview == "" {
		// 详情确认无视频附件：从发现列表剔除，避免一直占着「发现」页
		m.mu.Lock()
		if _, ok := m.state.Discovered[topic.TopicID]; ok {
			delete(m.state.Discovered, topic.TopicID)
			m.saveState()
		}
		m.mu.Unlock()
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusSkipped; t.Error = "no video attachment" })
		return errors.New("no video attachment")
	}
	full, err := m.client.ResolveFullM3u8(preview)
	if err != nil {
		if isCanceled(ctx) {
			m.setTask(topic.TopicID, taskCancelled(topic.TopicID, ctx))
			return errTaskCanceled
		}
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusFailed; t.Error = err.Error(); t.SpeedBps = 0 })
		return fmt.Errorf("resolve full m3u8: %w", err)
	}
	m.emit("info", "帖子 %d: %s", topic.TopicID, truncateRunes(topic.Title, 40))
	m.emit("dim", "  完整列表: %s", lastPathSegment(full))

	title := detail.Title
	if title == "" {
		title = topic.Title
	}
	if isCanceled(ctx) {
		m.setTask(topic.TopicID, taskCancelled(topic.TopicID, ctx))
		return errTaskCanceled
	}
	// 按作者分文件夹：videos/<作者名>/<标题-作者名>.mp4
	authorFolder := safeName(m.authorName(topic.AuthorUID), 40)
	authorDir := filepath.Join(m.paths.OutDir, authorFolder)
	if err := os.MkdirAll(authorDir, 0o755); err != nil {
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusFailed; t.Error = err.Error(); t.SpeedBps = 0 })
		return fmt.Errorf("create author dir: %w", err)
	}
	// 命名格式：视频标题-作者名字.mp4（重名时追加帖子 ID）
	base := safeName(title, 60) + "-" + safeName(m.authorName(topic.AuthorUID), 40)
	fname := base + ".mp4"
	outPath := filepath.Join(authorDir, fname)
	if _, err := os.Stat(outPath); err == nil {
		fname = fmt.Sprintf("%s_%d.mp4", base, topic.TopicID)
		outPath = filepath.Join(authorDir, fname)
	}

	m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusDownloading })
	err = m.dl.Download(full, outPath, downloader.Options{
		Ctx: ctx,
		OnProgress: func(p downloader.Progress) {
			m.setTask(topic.TopicID, func(t *Task) {
				t.SegDone, t.SegTotal = p.Done, p.Total
				if p.Total > 0 {
					t.Progress = float64(p.Done) / float64(p.Total) * 100
				}
				t.BytesDone = p.Bytes
				t.SpeedBps = p.SpeedBps
			})
		},
	})
	if err != nil {
		if isCanceled(ctx) {
			m.setTask(topic.TopicID, taskCancelled(topic.TopicID, ctx))
			// 清理可能残留的临时 TS 与半成品 mp4
			cleanPartial(outPath)
			m.emit("dim", "帖子 %d 下载已取消（已下载 %.1f MB）", topic.TopicID, float64(t.BytesDone)/1048576)
			return errTaskCanceled
		}
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusFailed; t.Error = err.Error(); t.SpeedBps = 0 })
		// 失败清理：临时 TS + remux 失败可能残留的半成品 mp4（视频库不出现坏文件）
		cleanPartial(outPath)
		m.emit("dim", "帖子 %d 失败，已清理临时文件", topic.TopicID)
		return err
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		m.setTask(topic.TopicID, func(t *Task) { t.Status = StatusFailed; t.Error = "输出文件缺失: " + err.Error() })
		return fmt.Errorf("stat output: %w", err)
	}
	sizeMB := float64(fi.Size()) / 1048576
	m.mu.Lock()
	m.state.Topics[topic.TopicID] = DownloadedRecord{
		AuthorUID:  topic.AuthorUID,
		Title:      title,
		CreateTime: topic.CreateTime,
		File:       authorFolder + "/" + fname, // 相对 videos/ 的子路径（统一正斜杠）
		DoneAt:     time.Now().Format("2006-01-02 15:04:05"),
	}
	m.saveState()
	m.mu.Unlock()

	m.addHistory(topic, fname, sizeMB, "done", "")

	m.setTask(topic.TopicID, func(t *Task) {
		t.Status = StatusDone
		t.Progress = 100
		t.SpeedBps = 0
		t.File = fname
		t.FinishedAt = time.Now()
	})
	m.emit("ok", "帖子 %d 完成: %s (%.1f MB)", topic.TopicID, fname, sizeMB)
	return nil
}

func lastPathSegment(u string) string {
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return u
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
