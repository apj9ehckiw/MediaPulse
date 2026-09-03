import { useState } from 'react'
import { Task, cancelTask } from '../api'
import { IconClose, IconHistory } from '../icons'
import { EmptyState } from './common'

const STATUS_TEXT: Record<Task['status'], string> = {
  pending: '等待',
  resolving: '解析中',
  downloading: '下载中',
  done: '完成',
  failed: '失败',
  skipped: '跳过',
  canceled: '已取消',
}

/** 下载速率展示：MB/s / KB/s */
function fmtSpeed(bps?: number): string {
  if (!bps || bps <= 0) return ''
  if (bps >= 1048576) return `${(bps / 1048576).toFixed(1)} MB/s`
  return `${(bps / 1024).toFixed(0)} KB/s`
}

/** 可取消的状态 */
const CANCELABLE = new Set<Task['status']>(['pending', 'resolving', 'downloading'])

export default function TaskList({ tasks }: { tasks: Task[] }) {
  const [canceling, setCanceling] = useState<Set<number>>(new Set())

  const onCancel = async (topicId: number) => {
    setCanceling((prev) => new Set(prev).add(topicId))
    try {
      await cancelTask(topicId)
    } catch (e) {
      alert(String(e).replace(/^Error:\s*/, ''))
    } finally {
      setCanceling((prev) => {
        const next = new Set(prev)
        next.delete(topicId)
        return next
      })
    }
  }

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconHistory size={14} /></span>
          任务队列
        </h3>
        <div className="side">
          <span className="badge">{tasks.length}</span>
        </div>
      </div>
      <div className="task-list">
        {tasks.length === 0 ? (
          <EmptyState
            title="暂无任务"
            hint="点击右上角「立即检查」扫描作者的最新视频；新视频会自动进入队列"
          />
        ) : (
          tasks.map((t) => (
            <div className="task" key={t.topicId}>
              <div className="row1">
                <span className={`status-chip ${t.status}`}>{STATUS_TEXT[t.status]}</span>
                <span className="title" title={t.title}>{t.title || `topic_${t.topicId}`}</span>
                <span className="meta">{t.createTime?.slice(0, 10)}</span>
                {CANCELABLE.has(t.status) && (
                  <button
                    className="btn ghost icon-only danger-ghost task-cancel"
                    onClick={() => onCancel(t.topicId)}
                    disabled={canceling.has(t.topicId)}
                    title="取消此任务"
                    aria-label={`取消任务 ${t.title || `topic_${t.topicId}`}`}
                  >
                    <IconClose size={13} />
                  </button>
                )}
              </div>
              {(t.status === 'downloading' || t.status === 'done') && (
                <div className={`progressbar ${t.status === 'done' ? 'done' : ''}`}>
                  <div className="fill" style={{ width: `${Math.max(t.progress, 3)}%` }} />
                </div>
              )}
              {t.status === 'resolving' && (
                <div className="progressbar indeterminate">
                  <div className="fill" style={{ width: '34%' }} />
                </div>
              )}
              <div className="meta">
                {t.status === 'downloading' && (
                  <span>
                    段 {t.segDone.toLocaleString()}/{t.segTotal.toLocaleString()} · {t.progress.toFixed(1)}%
                    {fmtSpeed(t.speedBps) ? ` · ${fmtSpeed(t.speedBps)}` : ''}
                    {t.authorUid ? ` · 作者 ${t.authorName || t.authorUid}` : ''}
                  </span>
                )}
                {t.status === 'pending' && (
                  <span>排队中{t.authorUid ? ` · 作者 ${t.authorName || t.authorUid}` : ''}</span>
                )}
                {t.status === 'done' && <span>{t.file}</span>}
                {t.status === 'failed' && <span className="err">{t.error}</span>}
                {t.status === 'skipped' && <span>无视频附件{t.error ? ` · ${t.error}` : ''}</span>}
                {t.status === 'canceled' && <span>已取消 · 可在「发现」页重新下载</span>}
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  )
}
