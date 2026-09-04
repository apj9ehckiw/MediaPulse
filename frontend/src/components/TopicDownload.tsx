import { useMemo, useState } from 'react'
import { downloadTopics } from '../api'
import { IconDownload } from '../icons'

interface Props {
  /** 下载入队后通知 App 刷新（任务队列即时可见） */
  onEnqueued: () => void
}

/**
 * 自定义下载页：批量输入帖子 URL 或 ID 直接创建下载任务。
 * 不依赖发现列表——适合下载未监控作者的帖子。
 */
export default function TopicDownload({ onEnqueued }: Props) {
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)

  // 输入预览：估算解析出的条目数（与后端解析规则一致：数字或含 /topic/<id> 的 URL）
  const parsedCount = useMemo(() => {
    const toks = input.split(/[\n\r\t ,，;]+/).map((s) => s.trim()).filter(Boolean)
    const seen = new Set<number>()
    let invalid = 0
    for (const tok of toks) {
      let id = 0
      if (/^\d+$/.test(tok)) {
        id = Number(tok)
      } else {
        const m = tok.match(/\/(?:topic|t)\/(\d+)/)
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
    try {
      const r = await downloadTopics(input)
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
    }
  }

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
              + '2180219, 2178519'
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
              {busy ? '解析入队中...' : '解析并下载'}
            </button>
          </div>
          {result && (
            <div className={`add-msg ${result.ok ? 'ok' : 'err'}`} role={result.ok ? 'status' : 'alert'}>
              {result.text}
            </div>
          )}
          <div className="hint">
            输入帖子 URL（形如 https://站点/topic/2096629）或直接帖子 ID，程序会逐个拉取详情：
            确认带视频后创建下载任务（无需先监控该作者）；已下载/队列中的自动跳过，
            无视频附件的会提示。下载进度见「概览」页任务队列。
          </div>
        </div>
      </div>
    </div>
  )
}

