import { useEffect, useRef } from 'react'
import { Event } from '../api'
import { IconClock } from '../icons'

const LEVEL_TAG: Record<string, string> = {
  info: 'INFO',
  ok: ' OK ',
  warn: 'WARN',
  error: 'FAIL',
  dim: 'DBG ',
}

export default function EventLog({ events, fullPage = false }: { events: Event[]; fullPage?: boolean }) {
  const boxRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight
  }, [events])

  return (
    <div className={`card log-card ${fullPage ? 'log-fullpage' : ''}`}>
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconClock size={14} /></span>
          运行日志
        </h3>
        <div className="side">
          <span className="badge">{events.length}</span>
        </div>
      </div>
      <div className="log" ref={boxRef}>
        {events.length === 0 ? (
          <div className="log-dim">等待事件流（SSE）...</div>
        ) : (
          events.map((e, i) => (
            <div key={i} className={`log-${e.level}`}>
              <span className="log-time">
                {new Date(e.time).toLocaleTimeString('zh-CN', { hour12: false })}
              </span>
              <span className={`lv lv-${e.level}`}>{LEVEL_TAG[e.level] ?? '·'}</span>
              {e.msg}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
