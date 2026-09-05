import { useMemo, useState } from 'react'
import { downloadTopics, TopicResolveItem } from '../api'
import { IconDownload } from '../icons'

interface Props {
  /** 下载入队后通知 App 刷新（任务队列即时可见） */
  onEnqueued: () => void
}

/** 单帖处理结果的展示文案 */
const OUTCOME_TEXT: Record<string, string> = {
  enqueued: '已入队',
  requeued: '已重新入队',
  downloaded: '已下载过',
  queued: '已在队列',
  no_video: '无视频',
  error: '获取失败',
  invalid: '无效',
}

/**
 * 自定义下载页：批量输入帖子 URL 或 ID 直接创建下载任务。
 * 不依赖发现列表——适合下载未监控作者的帖子。
 * 点击「解析并下载」后实时显示解析进度（正在解析 X/Y + 进度条 + 逐条结果）。
 */
export default function TopicDownload({ onEnqueued }: Props) {
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  // 解析进度（busy 期间有效）
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
  // 最近处理的帖子（新→旧，最多留 5 条让用户看到逐条结果）
  const [recent, setRecent] = useState<TopicResolveItem[]>([])

  // 输入预览：估算解析出的条目数（与后端解析规则一致：数字或含 /topic(s)/<id> 的 URL）
  const parsedCount = useMemo(() => {
    const toks = input.split(/[\n\r\t ,，;]+/).map((s) => s.trim()).filter(Boolean)
    const seen = new Set<number>()
    let invalid = 0
    for (const tok of toks) {
      let id = 0
      if (/^\d+$/.test(tok)) {
        id = Number(tok)
      } else {
        const m = tok.match(/\/(?:topics?|t)\/(\d+)/)
        if (m) id = Number(m[1])
      }
      if (id > 0) seen.add(id)
      else invalid++
    }
    return { total: seen.size, invalid }
  }, [input])

  const submit = async () => {
    if (!input.trim() || busy) return
    setBusy(true)
    setResult(null)
    setProgress(null)
    setRecent([])
    try {
      const r = await downloadTopics(input, (item) => {
        setProgress({ done: item.done, total: item.total })
        setRecent((prev) => [item, ...prev].slice(0, 5))
      })
      const parts: string[] = []
      if (r.enqueued > 0) parts.push(`${r.enqueued} 个已加入下载队列`)
      if (r.skipped > 0) parts.push(`${r.skipped} 个跳过（已下载/队列中/无视频）`)
      if (r.invalid > 0) parts.push(`${r.invalid} 条无法解析`)
      setResult({
        ok: r.enqueued > 0,
        text: parts.length > 0 ? parts.join(' · ') : '没有可下载的帖子',
      })
      if (r.enqueued > 0) {
        setInput('')
        onEnqueued()
      }
    } catch (e) {
      setResult({ ok: false, text: String(e).replace(/^Error:\s*/, '') })
    } finally {
      setBusy(false)
      setProgress(null)
    }
  }

  const pct = progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0

  return (
    <div className="settings-wrap page">
      <div className="card">
        <div className="card-head">
          <h3>
            <span className="h-icon"><IconDownload size={14} /></span>
            自定义下载
          </h3>
          <div className="side">
            {parsedCount.total > 0 && (
              <span className="badge">
                解析到 {parsedCount.total} 个帖子
                {parsedCount.invalid > 0 ? ` · ${parsedCount.invalid} 条无效` : ''}
              </span>
            )}
          </div>
        </div>
        <div className="settings-body">
          <textarea
            className="input td-input"
            placeholder={
              '每行一个，也支持空格 / 逗号分隔：\n'
              + '2096629\n'
              + 'https://example.com/topic/2180219\n'
              + 'https://haijiao.ai/topics/727618'
            }
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) submit()
            }}
            rows={8}
            aria-label="帖子 URL 或 ID 列表"
            disabled={busy}
          />
          <div className="td-actions">
            <span className="hint-inline">Ctrl+Enter 快速提交 · 单次最多 100 个</span>
            <button className="btn primary" onClick={submit} disabled={busy || !input.trim()}>
              {busy
                ? (progress ? `解析中 ${progress.done}/${progress.total}` : '解析入队中...')
                : '解析并下载'}
            </button>
          </div>
          {busy && (
            <div className="td-progress" role="status" aria-live="polite">
              <div className="td-progress-head">
                <span>正在逐个拉取帖子详情核验…</span>
                {progress && <span className="td-progress-count">{progress.done}/{progress.total}（{pct}%）</span>}
              </div>
              <div
                className={`progressbar ${progress ? '' : 'indeterminate'}`}
                role="progressbar"
                aria-valuenow={progress ? pct : undefined}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <div className="fill" style={{ width: progress ? `${Math.max(pct, 2)}%` : '34%' }} />
              </div>
              {recent.length > 0 && (
                <div className="td-recent">
                  {recent.map((it) => (
                    <div key={`${it.topicId}-${it.done}`} className="td-recent-row">
                      <span className="td-recent-id">#{it.topicId}</span>
                      <span className={`td-outcome td-outcome-${it.outcome}`}>
                        {OUTCOME_TEXT[it.outcome] ?? it.outcome}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
          {result && (
            <div className={`add-msg ${result.ok ? 'ok' : 'err'}`} role={result.ok ? 'status' : 'alert'}>
              {result.text}
            </div>
          )}
          <div className="hint">
            输入帖子 URL（形如 https://站点/topic/2096629 或 https://haijiao.ai/topics/727618）
            或直接帖子 ID，程序会逐个拉取详情：确认带视频后创建下载任务（无需先监控该作者）；
            已下载/队列中的自动跳过，无视频附件的会提示。下载进度见「任务」页。
          </div>
        </div>
      </div>
    </div>
  )
}

