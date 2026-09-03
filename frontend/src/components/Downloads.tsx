import { useCallback, useEffect, useState } from 'react'
import { clearDownloads, deleteDownload, DownloadRecord, fetchDownloads, videoFileURL } from '../api'
import { IconClose, IconDownload } from '../icons'
import { EmptyState, LinesSkeleton } from './common'

const STATUS_TEXT: Record<string, string> = {
  done: '成功',
  failed: '失败',
  skipped: '跳过',
}

type Filter = 'all' | 'done' | 'failed' | 'skipped'

export default function Downloads() {
  const [rows, setRows] = useState<DownloadRecord[] | null>(null)
  const [clearing, setClearing] = useState(false)
  const [deleting, setDeleting] = useState<string | null>(null)

  const reload = useCallback(() => {
    fetchDownloads(0).then(setRows).catch(() => setRows([]))
  }, [])

  useEffect(() => {
    reload()
  }, [reload])

  const clear = async (filter: Filter, n: number) => {
    const label = filter === 'all' ? '全部' : STATUS_TEXT[filter]
    if (!window.confirm(
      `确定删除${label}下载记录 ${n} 条吗？\n`
      + '对应帖子的下载去重记录会一并清除（之后可重新检查、重新下载）；已下载的视频文件保留在磁盘上。此操作不可恢复。',
    )) return
    setClearing(true)
    try {
      await clearDownloads(filter)
      reload()
    } finally {
      setClearing(false)
    }
  }

  const removeOne = async (r: DownloadRecord) => {
    if (!window.confirm(
      `确定删除「${r.title || `topic_${r.topicId}`}」的${STATUS_TEXT[r.status]}记录吗？\n`
      + (r.status === 'done'
        ? '该帖子的下载去重记录会一并清除（之后可重新检查、重新下载）；视频文件保留在磁盘上。'
        : '此操作不可恢复。'),
    )) return
    setDeleting(`${r.topicId}-${r.at}`)
    try {
      await deleteDownload(r.topicId, r.status, r.at)
      reload()
    } finally {
      setDeleting(null)
    }
  }

  if (!rows) {
    return (
      <div className="card">
        <LinesSkeleton rows={6} />
      </div>
    )
  }

  const doneN = rows.filter((r) => r.status === 'done').length
  const failN = rows.filter((r) => r.status === 'failed').length
  const skipN = rows.filter((r) => r.status === 'skipped').length

  return <DownloadsView rows={rows} counts={{ doneN, failN, skipN }} clearing={clearing} onClear={clear} deleting={deleting} onDelete={removeOne} />
}

function DownloadsView({ rows, counts, clearing, onClear, deleting, onDelete }: {
  rows: DownloadRecord[]
  counts: { doneN: number; failN: number; skipN: number }
  clearing: boolean
  onClear: (filter: Filter, n: number) => Promise<void>
  deleting: string | null
  onDelete: (r: DownloadRecord) => Promise<void>
}) {
  const [filter, setFilter] = useState<Filter>('all')
  const filtered = rows.filter((r) => filter === 'all' || r.status === filter)

  const filters: { key: Filter; label: string }[] = [
    { key: 'all', label: `全部 ${rows.length}` },
    { key: 'done', label: `成功 ${counts.doneN}` },
    { key: 'failed', label: `失败 ${counts.failN}` },
    { key: 'skipped', label: `跳过 ${counts.skipN}` },
  ]

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconDownload size={14} /></span>
          下载记录
        </h3>
        <div className="side">
          {counts.failN > 0 && <span className="badge err">{counts.failN} 失败</span>}
          <span className="badge ok">{counts.doneN} 成功</span>
        </div>
      </div>
      <div className="filter-row">
        {filters.map((f) => (
          <button
            key={f.key}
            className={`chip-btn ${filter === f.key ? 'active' : ''}`}
            onClick={() => setFilter(f.key)}
          >
            {f.label}
          </button>
        ))}
        <button
          className="btn ghost danger-ghost"
          style={{ marginLeft: 'auto' }}
          onClick={() => onClear(filter, filtered.length)}
          disabled={clearing || filtered.length === 0}
          title={filter === 'all' ? '删除全部下载记录' : '删除当前筛选的下载记录'}
        >
          {clearing ? '清理中...' : filter === 'all' ? '清空记录' : `清理${STATUS_TEXT[filter]}记录`}
        </button>
      </div>
      {filtered.length === 0 ? (
        <EmptyState title="暂无记录" hint="每次下载尝试（成功/失败/跳过）都会记录在这里" />
      ) : (
        <div className="dl-list">
          {filtered.map((r, i) => (
            <div className="dl-item" key={`${r.topicId}-${i}`}>
              <span className={`status-chip ${r.status}`}>{STATUS_TEXT[r.status] ?? r.status}</span>
              <div className="dl-body">
                <div className="t">{r.title || `topic_${r.topicId}`}</div>
                <div className="m">
                  作者 {r.authorName || r.authorUid} · 帖子 {r.topicId}
                  {r.createTime ? ` · 发布 ${r.createTime.slice(0, 10)}` : ''}
                  {r.sizeMB > 0 && ` · ${r.sizeMB.toFixed(1)} MB`}
                  {' · '}
                  {new Date(r.at).toLocaleString('zh-CN', { hour12: false })}
                  {r.error && <span className="err"> · {r.error}</span>}
                </div>
              </div>
              {r.status === 'done' && (
                <button
                  className="btn ghost"
                  onClick={() => window.open(videoFileURL(r.file), '_blank')}
                >
                  打开
                </button>
              )}
              <button
                className="btn ghost icon-only danger-ghost"
                style={{ padding: 6 }}
                onClick={() => onDelete(r)}
                disabled={deleting === `${r.topicId}-${r.at}`}
                title="删除这条记录"
                aria-label={`删除记录 ${r.title || `topic_${r.topicId}`}`}
              >
                <IconClose size={13} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
