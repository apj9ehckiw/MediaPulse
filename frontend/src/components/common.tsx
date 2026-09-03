// 通用小组件：骨架屏 / 空状态 / 数字动效

import { useEffect, useRef, useState } from 'react'
import { IconInbox } from '../icons'

/** 数字滚动动效 */
export function useCountUp(target: number, duration = 600): number {
  const [val, setVal] = useState(target)
  const fromRef = useRef(target)
  const rafRef = useRef(0)

  useEffect(() => {
    const from = fromRef.current
    if (from === target) return
    const start = performance.now()
    const tick = (now: number) => {
      const p = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - p, 3)
      setVal(from + (target - from) * eased)
      if (p < 1) {
        rafRef.current = requestAnimationFrame(tick)
      } else {
        fromRef.current = target
      }
    }
    rafRef.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(rafRef.current)
  }, [target, duration])

  return val
}

/** sparkline 迷你趋势线（纯 SVG） */
export function Sparkline({ values, height = 30 }: { values: number[]; height?: number }) {
  if (values.length < 2) return null
  const w = 120
  const max = Math.max(...values, 1)
  const step = w / (values.length - 1)
  const pts = values.map((v, i) => `${(i * step).toFixed(1)},${(height - 3 - (v / max) * (height - 6)).toFixed(1)}`)
  const line = pts.join(' ')
  const area = `M0,${height} L${line.split(' ').join(' L')} L${w},${height} Z`
  return (
    <svg width="100%" height={height} viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" style={{ display: 'block' }}>
      <defs>
        <linearGradient id="spark-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="rgba(37,99,235,0.22)" />
          <stop offset="100%" stopColor="rgba(37,99,235,0)" />
        </linearGradient>
      </defs>
      <path d={area} fill="url(#spark-fill)" stroke="none" />
      <path d={`M${line}`} fill="none" stroke="#2563eb" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/** 骨架屏卡片组 */
export function StatSkeleton({ n = 4 }: { n?: number }) {
  return (
    <>
      {Array.from({ length: n }, (_, i) => (
        <div className="skel" key={i} />
      ))}
    </>
  )
}

/** 卡片内骨架行 */
export function LinesSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div>
      {Array.from({ length: rows }, (_, i) => (
        <div className="skel line" key={i} style={{ width: `${88 - i * 12}%`, opacity: 1 - i * 0.15 }} />
      ))}
    </div>
  )
}

/** 空状态 */
export function EmptyState({ title, hint, icon }: { title: string; hint?: string; icon?: React.ReactNode }) {
  return (
    <div className="empty">
      <div className="e-icon">{icon ?? <IconInbox size={26} strokeWidth={1.5} />}</div>
      <div className="e-title">{title}</div>
      {hint && <div className="e-hint">{hint}</div>}
    </div>
  )
}
