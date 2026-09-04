import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Event,
  fetchAuthState,
  fetchStatus,
  fetchVideos,
  logout,
  triggerCheck,
  VideoInfo,
  videoFileURL,
  AuthMode,
} from './api'
import { IconClose, IconClock, IconDashboard, IconDownload, IconGlobe, IconHistory, IconLogout, IconRefresh, IconSettings, IconSearch, IconUsers, IconVideo, IconPlus } from './icons'
import StatsCards from './components/StatsCards'
import TasksPage from './components/TasksPage'
import VideoLibrary from './components/VideoLibrary'
import EventLog from './components/EventLog'
import Settings from './components/Settings'
import Downloads from './components/Downloads'
import Discovered from './components/Discovered'
import Authors from './components/Authors'
import TopicDownload from './components/TopicDownload'
import Login from './components/Login'
import Setup from './components/Setup'
import { EmptyState } from './components/common'
import { Task } from './api'

type Tab = 'dashboard' | 'library' | 'tasks' | 'discovered' | 'authors' | 'downloads' | 'custom' | 'logs' | 'settings'

const TABS: { key: Tab; label: string; icon: React.ReactNode }[] = [
  { key: 'dashboard', label: '概览', icon: <IconDashboard size={16} /> },
  { key: 'library', label: '视频库', icon: <IconVideo size={16} /> },
  { key: 'tasks', label: '任务', icon: <IconHistory size={16} /> },
  { key: 'discovered', label: '发现', icon: <IconSearch size={16} /> },
  { key: 'authors', label: '作者', icon: <IconUsers size={16} /> },
  { key: 'downloads', label: '下载记录', icon: <IconDownload size={16} /> },
  { key: 'custom', label: '自定义下载', icon: <IconPlus size={16} /> },
  { key: 'logs', label: '日志', icon: <IconClock size={16} /> },
  { key: 'settings', label: '设置', icon: <IconSettings size={16} /> },
]

const TAB_META: Record<Tab, { title: string; crumb: string }> = {
  dashboard: { title: '概览', crumb: '监控状态 · 进行中下载 · 最近动态' },
  library: { title: '视频库', crumb: '已下载视频 · 点击播放' },
  tasks: { title: '任务', crumb: '进行中与已结束的下载任务' },
  discovered: { title: '发现', crumb: '检查到的新视频 · 按时间筛选手动下载' },
  authors: { title: '作者', crumb: '添加 · 开关 · 删除作者 · 检查状态' },
  downloads: { title: '下载记录', crumb: '每次下载的流水明细' },
  custom: { title: '自定义下载', crumb: '输入帖子 URL / ID 直接下载 · 支持批量' },
  logs: { title: '运行日志', crumb: '实时事件流（SSE）' },
  settings: { title: '设置', crumb: '下载参数（网页端持久化）' },
}

export default function App() {
  const [tab, setTab] = useState<Tab>('dashboard')
  const [authState, setAuthState] = useState<AuthMode | 'unknown'>('unknown')
  const [snapshot, setSnapshot] = useState<Awaited<ReturnType<typeof fetchStatus>> | null>(null)
  const [videos, setVideos] = useState<VideoInfo[]>([])
  const [events, setEvents] = useState<Event[]>([])
  const [playing, setPlaying] = useState<VideoInfo | null>(null)
  const [downloadsVersion, setDownloadsVersion] = useState(0)
  const [discoveredVersion, setDiscoveredVersion] = useState(0)
  const [checkWaiting, setCheckWaiting] = useState(false)
  const [version, setVersion] = useState('')
  const waitStartRef = useRef(0)
  const esRef = useRef<EventSource | null>(null)

  const refresh = useCallback(async () => {
    const r = await fetch('/api/status')
    if (r.status === 401 || r.status === 403) {
      // 会话失效或需要初始化：重新拉取鉴权状态
      fetchAuthState().then(setAuthState)
      return
    }
    const [d, v] = await Promise.all([r.json(), fetchVideos()])
    const s = d.snapshot
    setSnapshot(s)
    setVideos(v)
    if (d.version) setVersion(d.version as string)
    // 检查已真正开始：交给后端 checking 状态；长时间未开始（如没有启用作者）：兜底释放
    if (s.checking) {
      setCheckWaiting(false)
      waitStartRef.current = 0
    } else if (waitStartRef.current && Date.now() - waitStartRef.current > 8000) {
      setCheckWaiting(false)
      waitStartRef.current = 0
    }
  }, [])

  // 先确定鉴权状态，再启动轮询与 SSE
  useEffect(() => {
    fetchAuthState().then(setAuthState)
  }, [])

  // 检查进行中时加密轮询（1s），让进度及时；平时 5s
  const checking = !!snapshot?.checking || checkWaiting
  const authed = authState === 'open' || authState === 'authed'
  useEffect(() => {
    if (!authed) return
    refresh()
    const t = setInterval(refresh, checking ? 1000 : 5000)
    return () => clearInterval(t)
  }, [authed, refresh, checking])

  useEffect(() => {
    if (!authed) return
    const es = new EventSource('/api/events?stream=1')
    esRef.current = es
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as Event
        setEvents((prev) => [...prev.slice(-299), ev])
        if (ev.level === 'ok' || ev.level === 'error') {
          setDownloadsVersion((v) => v + 1)
          setDiscoveredVersion((v) => v + 1)
        }
      } catch { /* ignore */ }
    }
    return () => { es.close(); esRef.current = null }
  }, [authed])

  const onLogout = async () => {
    try { await logout() } catch { /* ignore */ }
    setSnapshot(null)
    setVideos([])
    setEvents([])
    setAuthState('login')
  }

  const onCheck = async () => {
    setCheckWaiting(true)
    waitStartRef.current = Date.now()
    try {
      await triggerCheck()
      setEvents((prev) => [...prev.slice(-299), {
        time: new Date().toISOString(), level: 'info', msg: '已触发手动检查...',
      }])
    } catch {
      setCheckWaiting(false)
      waitStartRef.current = 0
    }
  }

  const hasActive = snapshot?.tasks.some(
    (t) => t.status === 'pending' || t.status === 'resolving' || t.status === 'downloading',
  ) ?? false

  const navBadge = (key: Tab): number | null => {
    if (key === 'tasks') return hasActive ? (snapshot?.tasks.filter((t) => t.status === 'downloading').length ?? 0) : null
    if (key === 'discovered') return snapshot?.discoveredCount ? snapshot.discoveredCount : null
    return null
  }

  if (authState === 'unknown') {
    return <div className="login-wrap"><div className="empty">加载中...</div></div>
  }
  if (authState === 'setup') {
    return <Setup onDone={() => setAuthState('authed')} />
  }
  if (authState === 'login') {
    return <Login onLogin={() => setAuthState('authed')} />
  }

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <img className="logo-img" src="/logo.png" alt="logo" />
          <div>
            <h1>媒体监控台</h1>
            <div className="sub">自动追更 · 解密下载</div>
          </div>
        </div>
        <nav className="nav">
          {TABS.map((t) => {
            const n = navBadge(t.key)
            return (
              <button
                key={t.key}
                className={`nav-item ${tab === t.key ? 'active' : ''}`}
                onClick={() => setTab(t.key)}
              >
                {t.icon}
                <span className="nav-label">{t.label}</span>
                {n ? <span className="badge">{n}</span> : null}
              </button>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <div className="meta-row version-row" title={`MediaPulse ${version || '...'}`}>
            <span className="version-tag">MediaPulse</span>
            <span className="version-num">{version || '…'}</span>
          </div>
          <div className="meta-row">
            <IconGlobe size={12} />
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {snapshot?.apiBase.replace(/^https?:\/\//, '') ?? '—'}
            </span>
          </div>
          <div className="meta-row">
            <IconDownload size={12} />
            <span>已下载 {snapshot?.downloaded ?? 0} · 库 {(snapshot ? (snapshot.totalMB >= 1024 ? (snapshot.totalMB / 1024).toFixed(1) + ' GB' : snapshot.totalMB.toFixed(0) + ' MB') : '—')}</span>
          </div>
          {snapshot?.authEnabled && (
            <button className="btn ghost logout-btn" onClick={onLogout} title="退出登录">
              <IconLogout size={13} />
              退出登录
            </button>
          )}
        </div>
      </aside>

      <main className="content">
        <header className="topbar">
          <div>
            <h2>{TAB_META[tab].title}</h2>
            <div className="crumb">{TAB_META[tab].crumb}</div>
          </div>
          <div className="topbar-actions">
            <span className={`pill ${hasActive || checking ? 'busy' : snapshot?.running ? 'on' : ''}`} role="status" aria-live="polite">
              <span className="dot" />
              {hasActive
                ? '下载中'
                : checking
                  ? (snapshot && snapshot.checkTotal > 0 ? `检查中 ${snapshot.checkDone}/${snapshot.checkTotal}` : '检查中')
                  : snapshot?.running ? '监控中' : '已停止'}
            </span>
            <button
              className={`btn primary check-btn ${checking ? 'checking' : ''}`}
              onClick={onCheck}
              disabled={checking || hasActive}
              title="扫描所有启用作者的最新视频"
            >
              <span className={checking ? 'spin' : undefined}>
                <IconRefresh size={14} />
              </span>
              {checking
                ? (snapshot && snapshot.checkTotal > 0
                    ? `检查中 ${snapshot.checkDone}/${snapshot.checkTotal}`
                    : '检查中...')
                : '立即检查'}
              {checking && (snapshot?.checkTotal ?? 0) > 0 && (
                <span
                  className="check-progress"
                  style={{ width: `${Math.max(Math.round(((snapshot?.checkDone ?? 0) / (snapshot?.checkTotal ?? 1)) * 100), 4)}%` }}
                />
              )}
              {checking && (snapshot?.checkTotal ?? 0) === 0 && (
                <span className="check-progress indeterminate" />
              )}
            </button>
          </div>
        </header>

        <div className="page" key={tab}>
          {tab === 'dashboard' && (
            <Dashboard snapshot={snapshot} events={events} />
          )}

          {tab === 'library' && (
            <VideoLibrary videos={videos} onPlay={setPlaying} />
          )}

          {tab === 'tasks' && (
            <TasksPage tasks={snapshot?.tasks ?? []} />
          )}

          {tab === 'discovered' && (
            <Discovered snapshot={snapshot} version={discoveredVersion} />
          )}

          {tab === 'authors' && (
            <Authors snapshot={snapshot} onRefresh={refresh} />
          )}

          {tab === 'downloads' && (
            <Downloads key={downloadsVersion} />
          )}

          {tab === 'custom' && (
            <TopicDownload onEnqueued={refresh} />
          )}

          {tab === 'logs' && (
            <EventLog events={events} fullPage />
          )}

          {tab === 'settings' && (
            <Settings
              onSaved={(msg) => setEvents((prev) => [...prev.slice(-299), {
                time: new Date().toISOString(), level: 'ok', msg,
              }])}
            />
          )}
        </div>
      </main>

      {playing && <PlayerModal video={playing} onClose={() => setPlaying(null)} />}
    </div>
  )
}

// ==========================================
// 概览页：统计卡 + 进行中任务摘要 + 最近事件摘要
// ==========================================
function Dashboard({ snapshot, events }: {
  snapshot: Awaited<ReturnType<typeof fetchStatus>> | null
  events: Event[]
}) {
  const tasks = snapshot?.tasks ?? []
  const active = tasks.filter((t) => t.status === 'pending' || t.status === 'resolving' || t.status === 'downloading')
  const recent = events.slice(-8).reverse()

  return (
    <>
      <StatsCards snapshot={snapshot} />
      <div className="dash">
        <div className="col">
          <RecentTasksCard active={active} />
        </div>
        <div className="col">
          <RecentEventsCard events={recent} />
        </div>
      </div>
    </>
  )
}

/** 概览：进行中任务摘要（跳转任务页看全部） */
function RecentTasksCard({ active }: { active: Task[] }) {
  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconHistory size={14} /></span>
          进行中的下载
        </h3>
        <div className="side">
          <span className="badge">{active.length}</span>
        </div>
      </div>
      <div className="task-list">
        {active.length === 0 ? (
          <EmptyState title="当前没有下载任务" hint="「发现」页选择下载，或用「自定义下载」输入帖子 URL/ID" />
        ) : (
          active.map((t) => (
            <div className="task" key={t.topicId}>
              <div className="row1">
                <span className={`status-chip ${t.status}`}>{t.status === 'downloading' ? '下载中' : t.status === 'resolving' ? '解析中' : '等待'}</span>
                <span className="title" title={t.title}>{t.title || `topic_${t.topicId}`}</span>
                {t.status === 'downloading' && <span className="meta">{Math.round(t.progress)}%</span>}
              </div>
              {(t.status === 'downloading') && (
                <div className="progressbar downloading">
                  <div className="fill" style={{ width: `${Math.max(t.progress, 2)}%` }} />
                </div>
              )}
              {t.status === 'resolving' && (
                <div className="progressbar indeterminate">
                  <div className="fill" style={{ width: '34%' }} />
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

/** 概览：最近事件摘要（跳转日志页看全部） */
function RecentEventsCard({ events }: { events: Event[] }) {
  return (
    <div className="card log-card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconClock size={14} /></span>
          最近动态
        </h3>
        <div className="side">
          <span className="badge">{events.length}</span>
        </div>
      </div>
      <div className="log">
        {events.length === 0 ? (
          <div className="log-dim">等待事件流（SSE）...</div>
        ) : (
          events.map((e, i) => (
            <div key={i} className={`log-${e.level}`}>
              <span className="log-time">
                {new Date(e.time).toLocaleTimeString('zh-CN', { hour12: false })}
              </span>
              {e.msg}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

// ==========================================
// 播放弹窗
// ==========================================
function PlayerModal({ video, onClose }: { video: VideoInfo; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-box" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span className="t">{video.title || `topic_${video.topicId}`}</span>
          <button className="btn ghost icon-only" onClick={onClose} title="关闭 (Esc)">
            <IconClose size={14} />
          </button>
        </div>
        <video src={videoFileURL(video.file)} controls autoFocus autoPlay />
        <div className="modal-foot">
          {video.authorUid ? <span>作者 {video.authorName || video.authorUid}</span> : null}
          <span>帖子 {video.topicId}</span>
          <span>发布 {video.createTime?.slice(0, 10) ?? '—'}</span>
          <span>{video.sizeMB >= 1024 ? `${(video.sizeMB / 1024).toFixed(2)} GB` : `${video.sizeMB.toFixed(1)} MB`}</span>
        </div>
      </div>
    </div>
  )
}

// 空态导出复用
export { EmptyState }

