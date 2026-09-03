// 共享类型（与 Go 后端 JSON 对齐）

export interface Task {
  topicId: number
  authorUid: number
  /** 作者昵称（配置备注，缺省回退 UID 字符串） */
  authorName?: string
  title: string
  createTime: string | null
  status: 'pending' | 'resolving' | 'downloading' | 'done' | 'failed' | 'skipped'
  progress: number
  segDone: number
  segTotal: number
  /** 下载瞬时速率（字节/秒，仅 downloading 状态） */
  speedBps?: number
  /** 已下载字节（累计） */
  bytesDone?: number
  file?: string
  error?: string
  addedAt: string
  finishedAt?: string
}

export interface DownloadedRecord {
  title: string
  createTime: string | null
  file: string
  done_at: string
  authorUid?: number
}

export interface AuthorStat {
  uid: number
  note: string
  enabled: boolean
  downloaded: number
  videos: number
  pending: number
  lastCheck?: string
}

export interface Snapshot {
  apiBase: string
  outDir: string
  intervalSec: number
  listType: number
  workers: number
  autoDownload: boolean
  authEnabled: boolean
  running: boolean
  checking: boolean
  /** 本轮检查已完成的作者数（检查进度） */
  checkDone: number
  /** 本轮检查的启用作者总数 */
  checkTotal: number
  downloaded: number
  discoveredCount: number
  totalMB: number
  tasks: Task[]
  records: Record<string, DownloadedRecord>
  authors: AuthorStat[]
}

export interface Event {
  time: string
  level: 'info' | 'ok' | 'warn' | 'error' | 'dim'
  msg: string
}

export interface VideoInfo {
  topicId: number
  authorUid?: number
  /** 作者昵称（配置备注，缺省回退 UID 字符串） */
  authorName?: string
  title: string
  createTime: string | null
  file: string
  sizeMB: number
  doneAt: string
}

export interface AuthorConfig {
  uid: number
  note: string
  enabled: boolean
}

export interface AppConfig {
  apiBase: string
  authors: AuthorConfig[]
  intervalSec: number
  listType: number
  workers: number
  autoDownload: boolean
  /** 自动下载仅限发布时间在该日期（YYYY-MM-DD，含当天之后）之后的帖子；空 = 不限制 */
  autoDownloadAfter?: string
  /** 服务端不回传真实密码；提交时非空 = 设置新密码 */
  password?: string
  hasPassword?: boolean
  /** 提交时为 true = 停用鉴权（清除密码） */
  clearPassword?: boolean
  authDisabled?: boolean
}

export interface FFmpegStatus {
  state: 'checking' | 'ready' | 'installing' | 'failed' | 'missing'
  path?: string
  error?: string
  goos: string
  goarch: string
}

export interface DiscoveredVideo {
  topicId: number
  authorUid: number
  title: string
  createTime: string | null
  discoveredAt: string
}

export interface DownloadRecord {
  topicId: number
  authorUid: number
  /** 作者名字（配置备注；作者已删除时后端回退为 UID 字符串） */
  authorName?: string
  title: string
  createTime: string
  file: string
  sizeMB: number
  status: 'done' | 'failed' | 'skipped'
  error?: string
  at: string
}

// ==========================================
// API
// ==========================================
export async function fetchStatus(): Promise<Snapshot> {
  const r = await fetch('/api/status')
  return r.json()
}

export async function fetchVideos(): Promise<VideoInfo[]> {
  const r = await fetch('/api/videos')
  return r.json()
}

export async function triggerCheck(): Promise<void> {
  await fetch('/api/check', { method: 'POST' })
}

export async function fetchConfig(): Promise<AppConfig> {
  const r = await fetch('/api/config')
  return r.json()
}

export async function updateConfig(cfg: AppConfig): Promise<AppConfig> {
  const r = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  })
  if (!r.ok) {
    throw new Error((await r.text()) || `HTTP ${r.status}`)
  }
  return r.json()
}

export async function fetchDownloads(limit = 0): Promise<DownloadRecord[]> {
  const r = await fetch(`/api/downloads?limit=${limit}`)
  return r.json()
}

export async function clearDownloads(status: 'all' | 'done' | 'failed' | 'skipped'): Promise<{ removed: number }> {
  const r = await fetch('/api/downloads/clear', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ status }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
  return r.json()
}

export async function deleteDownload(topicId: number, status: 'done' | 'failed' | 'skipped', at: string): Promise<{ dedup: number }> {
  const r = await fetch('/api/downloads/delete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topicId, status, at }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
  return r.json()
}

export async function fetchDiscovered(): Promise<DiscoveredVideo[]> {
  const r = await fetch('/api/discovered')
  return r.json()
}

export async function requestDownloads(topicIds: number[]): Promise<{ enqueued: number }> {
  const r = await fetch('/api/download', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topicIds }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
  return r.json()
}

export async function dismissDiscovered(topicIds: number[]): Promise<void> {
  const r = await fetch('/api/discovered/dismiss', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topicIds }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
}

export async function addAuthorRemote(uid: number): Promise<{ uid: number; name: string }> {
  const r = await fetch('/api/authors/add', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ uid }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
  return r.json()
}

export async function setAuthorEnabled(uid: number, enabled: boolean): Promise<{ uid: number; name: string; enabled: boolean }> {
  const r = await fetch('/api/authors/enable', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ uid, enabled }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
  return r.json()
}

export async function removeAuthorRemote(uid: number): Promise<void> {
  const r = await fetch('/api/authors/remove', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ uid }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
}

export function videoFileURL(file: string): string {
  return '/api/videos/file/' + encodeURIComponent(file)
}

// ==========================================
// 鉴权
// ==========================================

export type AuthMode = 'setup' | 'login' | 'open' | 'authed'

/**
 * 当前鉴权状态：
 * - setup：首次部署待设置密码
 * - login：需登录
 * - open：无需鉴权
 * - authed：已设密码且当前会话 Cookie 有效（刷新免登录，直接进主界面）
 */
export async function fetchAuthState(): Promise<AuthMode> {
  const r = await fetch('/api/auth/state')
  const d = await r.json()
  return d.mode as AuthMode
}

/** 首次部署设置访问密码（成功后自动登录） */
export async function setupAuth(password: string): Promise<void> {
  const r = await fetch('/api/auth/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
}

export async function login(password: string): Promise<void> {
  const r = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
}

export async function logout(): Promise<void> {
  await fetch('/api/auth/logout', { method: 'POST' })
}

// ==========================================
// ffmpeg
// ==========================================

export async function fetchFFmpeg(): Promise<FFmpegStatus> {
  const r = await fetch('/api/ffmpeg')
  return r.json()
}

export async function installFFmpeg(): Promise<void> {
  const r = await fetch('/api/ffmpeg/install', { method: 'POST' })
  if (!r.ok) throw new Error((await r.text()) || `HTTP ${r.status}`)
}
