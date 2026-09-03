import { useCallback, useEffect, useMemo, useState } from 'react'
import { DiscoveredVideo, dismissDiscovered, fetchDiscovered, requestDownloads, Snapshot } from '../api'
import { IconClock, IconDownload } from '../icons'
import { EmptyState } from './common'
import AuthorFilterBar from './AuthorFilterBar'

interface Props {
  snapshot: Snapshot | null
  /** App 层 SSE 事件计数，变化时重新拉取 */
  version: number
}

type TaskStatus = NonNullable<Snapshot['tasks']>[number]['status']

const PRESETS: { label: string; days?: number }[] = [
  { label: '全部' },
  { label: '近24小时', days: 1 },
  { label: '近7天', days: 7 },
  { label: '近30天', days: 30 },
]

function fmtLocal(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

/** 帖子发布时间（"2026-05-20 09:42:51"）转 Date */
function pubTime(s: string | null): Date | null {
  if (!s) return null
  const d = new Date(s.replace(' ', 'T'))
  return isNaN(d.getTime()) ? null : d
}

const STATUS_TEXT: Record<TaskStatus, string> = {
  pending: '排队中',
  resolving: '解析中',
  downloading: '下载中',
  done: '已完成',
  failed: '失败',
  skipped: '已跳过',
  canceled: '已取消',
}

export default function Discovered({ snapshot, version }: Props) {
  const [rows, setRows] = useState<DiscoveredVideo[] | null>(null)
  const [filter, setFilter] = useState('') // datetime-local 值，空 = 全部
  const [authorFilter, setAuthorFilter] = useState<Set<number>>(new Set()) // 空集合 = 全部作者（多选并集）
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [err, setErr] = useState('')

  const refresh = useCallback(async () => {
    try {
      const list = await fetchDiscovered()
      setRows(list)
    } catch (e) {
      setErr(String(e))
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh, version])

  // 任务状态交叉引用：显示队列进度、避免重复下载
  const taskStatus = useMemo(() => {
    const m = new Map<number, TaskStatus>()
    for (const t of snapshot?.tasks ?? []) m.set(t.topicId, t.status)
    return m
  }, [snapshot])

  const authorName = useCallback((uid: number) => {
    const a = snapshot?.authors.find((x) => x.uid === uid)
    return a?.note || (uid ? String(uid) : '—')
  }, [snapshot])

  // 作者筛选选项：只列出当前发现记录里实际存在的作者，附条数；按作者页顺序排
  const authorOptions = useMemo(() => {
    const counts = new Map<number, number>()
    for (const r of rows ?? []) {
      if (r.authorUid > 0) counts.set(r.authorUid, (counts.get(r.authorUid) ?? 0) + 1)
    }
    const order = new Map((snapshot?.authors ?? []).map((a, i) => [a.uid, i]))
    return [...counts.keys()]
      .sort((a, b) => (order.get(a) ?? Number.MAX_SAFE_INTEGER) - (order.get(b) ?? Number.MAX_SAFE_INTEGER) || a - b)
      .map((uid) => ({ uid, count: counts.get(uid) as number }))
  }, [rows, snapshot])

  // 已选作者全部不再出现在发现记录（下载完/忽略掉）时自动清掉无效选择
  useEffect(() => {
    if (authorFilter.size === 0) return
    const alive = new Set(authorOptions.map((o) => o.uid))
    const next = new Set([...authorFilter].filter((uid) => alive.has(uid)))
    if (next.size !== authorFilter.size) setAuthorFilter(next)
  }, [authorOptions, authorFilter])

  const filtered = useMemo(() => {
    if (!rows) return []
    let list = rows
    if (authorFilter.size > 0) {
      list = list.filter((r) => authorFilter.has(r.authorUid))
    }
    if (filter) {
      const limit = new Date(filter).getTime()
      list = list.filter((r) => {
        const t = pubTime(r.createTime)
        return t !== null && t.getTime() >= limit
      })
    }
    return list
  }, [rows, filter, authorFilter])

  // 是否有任何筛选条件（作者多选或时间）：决定头部徽标显示 筛选数/总数
  const hasFilter = authorFilter.size > 0 || filter !== ''

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const allSelected = filtered.length > 0 && filtered.every((r) => selected.has(r.topicId))

  const toggleAll = () => {
    setSelected((prev) => {
      if (filtered.length > 0 && filtered.every((r) => prev.has(r.topicId))) {
        const next = new Set(prev)
        for (const r of filtered) next.delete(r.topicId)
        return next
      }
      const next = new Set(prev)
      for (const r of filtered) next.add(r.topicId)
      return next
    })
  }

  const flash = (msg: string) => {
    setNotice(msg)
    setErr('')
    setTimeout(() => setNotice(''), 4000)
  }

  const download = async (ids: number[]) => {
    if (ids.length === 0) return
    setBusy(true)
    try {
      const { enqueued } = await requestDownloads(ids)
      flash(enqueued > 0 ? `已加入下载队列 ${enqueued} 个` : '所选视频均已在队列或已下载')
      setSelected(new Set())
      refresh()
    } catch (e) {
      setErr(String(e))
    } finally {
      setBusy(false)
    }
  }

  const dismiss = async (ids: number[]) => {
    if (ids.length === 0) return
    setBusy(true)
    try {
      await dismissDiscovered(ids)
      flash(`已忽略 ${ids.length} 条`)
      setSelected(new Set())
      refresh()
    } catch (e) {
      setErr(String(e))
    } finally {
      setBusy(false)
    }
  }

  const selectedN = selected.size

  // 最近一次检查时间：取所有作者 lastCheck 的最新值（检查完成后刷新）
  const lastCheckAt = useMemo(() => {
    let max = ''
    for (const a of snapshot?.authors ?? []) {
      if (a.lastCheck && a.lastCheck > max) max = a.lastCheck
    }
    return max || null
  }, [snapshot])

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconDownload size={14} /></span>
          发现待下载
        </h3>
        <div className="side">
          {lastCheckAt && (
            <span className="badge with-icon" title={`最近一次检查：${lastCheckAt}`}>
              <IconClock size={11} />
              检查于 {lastCheckAt.slice(5, 16)}
            </span>
          )}
          {rows && (
            <span className="badge">
              {hasFilter ? `${filtered.length} / ${rows.length} 条` : `${rows.length} 条`}
            </span>
          )}
        </div>
      </div>

      <div className="disc-toolbar">
        <div className="filter-row" role="group" aria-label="按发布时间筛选">
          {PRESETS.map((p) => {
            const val = p.days ? fmtLocal(new Date(Date.now() - p.days * 86400000)) : ''
            return (
              <button
                key={p.label}
                className={`chip-btn ${filter === val ? 'active' : ''}`}
                onClick={() => setFilter(val)}
              >
                {p.label}
              </button>
            )
          })}
          <label className="disc-time">
            <IconClock size={13} />
            <span>发布时间晚于</span>
            <input
              className="input"
              type="datetime-local"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </label>
          <AuthorFilterBar
            options={authorOptions.map((o) => ({ uid: o.uid, name: authorName(o.uid), count: o.count }))}
            selected={authorFilter}
            onChange={setAuthorFilter}
          />
        </div>
        <div className="disc-actions">
          {notice && <span className="disc-notice" role="status">{notice}</span>}
          {err && <span className="disc-err" role="alert">{err}</span>}
          <button className="btn ghost" onClick={toggleAll} disabled={filtered.length === 0}>
            {allSelected ? '取消全选' : '全选筛选结果'}
          </button>
          <button
            className="btn ghost danger-ghost"
            onClick={() => dismiss([...selected])}
            disabled={selectedN === 0 || busy}
          >
            忽略选中
          </button>
          <button
            className="btn primary"
            onClick={() => download([...selected])}
            disabled={selectedN === 0 || busy}
          >
            <IconDownload size={13} />
            下载选中{selectedN > 0 ? ` (${selectedN})` : ''}
          </button>
        </div>
      </div>

      {!rows ? (
        <div className="disc-list">
          <div className="skel line" style={{ width: '88%' }} />
          <div className="skel line" style={{ width: '76%' }} />
          <div className="skel line" style={{ width: '82%' }} />
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          title={rows.length === 0 ? '暂无发现' : '没有符合筛选条件的帖子'}
          hint={rows.length === 0
            ? '自动下载默认关闭；监控检查到的新视频会出现在这里，按时间筛选后手动选择下载'
            : '调整作者或时间筛选条件，或选择「全部」'}
        />
      ) : (
        <div className="dl-list">
          {filtered.map((r) => {
            const st = taskStatus.get(r.topicId)
            const active = st === 'pending' || st === 'resolving' || st === 'downloading'
            return (
              <div className="dl-item" key={r.topicId}>
                <input
                  type="checkbox"
                  className="row-check"
                  checked={selected.has(r.topicId)}
                  onChange={() => toggle(r.topicId)}
                  aria-label={`选择 ${r.title || `topic_${r.topicId}`}`}
                />
                <div className="dl-body">
                  <div className="t">{r.title || `topic_${r.topicId}`}</div>
                  <div className="m">
                    {r.authorUid > 0 && <>作者 {authorName(r.authorUid)} · </>}
                    发布 {r.createTime?.slice(0, 10) ?? '—'}
                    {' · 发现 '}
                    {r.discoveredAt.slice(5, 16)}
                  </div>
                </div>
                {st && (
                  <span className={`status-chip ${st}`}>{STATUS_TEXT[st]}</span>
                )}
                <button
                  className={`btn ${active ? 'ghost' : 'primary'}`}
                  onClick={() => download([r.topicId])}
                  disabled={active || busy}
                  title={active ? '已在下载队列中' : st === 'failed' || st === 'skipped' || st === 'canceled' ? '重新下载' : '下载此视频'}
                >
                  {active ? '队列中' : st === 'failed' || st === 'skipped' || st === 'canceled' ? '重试' : '下载'}
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
