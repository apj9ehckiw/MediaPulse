import { Snapshot } from '../api'
import { IconDownload, IconFolder, IconHistory, IconUsers } from '../icons'
import { Sparkline, StatSkeleton, useCountUp } from './common'

interface Props {
  snapshot: Snapshot | null
  /** 每轮下载增量记录（时间序），用于 sparkline */
  downloadTicks?: number[]
}

function StatCard({ label, icon, tone, value, unit, spark, hint, wide }: {
  label: string
  icon: React.ReactNode
  tone: 'c1' | 'c2' | 'c3' | 'c4'
  value: string
  unit?: string
  spark?: React.ReactNode
  hint?: string
  wide?: boolean
}) {
  return (
    <div className={`stat ${wide ? 'wide' : ''}`}>
      <div className="label-row">
        <span className={`chip ${tone}`}>{icon}</span>
        {label}
      </div>
      <div className={`value ${tone === 'c1' ? 'tone-ok' : tone === 'c2' ? 'tone-accent' : ''}`}>
        {value}
        {unit && <span className="unit">{unit}</span>}
      </div>
      {spark && <div className="spark">{spark}</div>}
      {hint && <div className="spark-hint">{hint}</div>}
    </div>
  )
}

export default function StatsCards({ snapshot, downloadTicks }: Props) {
  if (!snapshot) {
    return <div className="stats-grid"><StatSkeleton /></div>
  }

  const activeN = snapshot.tasks.filter(
    (t) => t.status === 'pending' || t.status === 'resolving' || t.status === 'downloading',
  ).length
  const enabledAuthors = snapshot.authors.filter((a) => a.enabled).length

  return (
    <div className="stats-grid">
      <StatCard
        label="已下载视频"
        icon={<IconDownload size={13} />}
        tone="c1"
        value={String(snapshot.downloaded)}
      />
      <StatCard
        label="库容量"
        icon={<IconFolder size={13} />}
        tone="c2"
        value={snapshot.totalMB >= 1024 ? (snapshot.totalMB / 1024).toFixed(2) : snapshot.totalMB.toFixed(1)}
        unit={snapshot.totalMB >= 1024 ? 'GB' : 'MB'}
      />
      <StatCard
        label="进行中任务"
        icon={<IconHistory size={13} />}
        tone="c3"
        value={String(activeN)}
      />
      <StatCard
        label="监控作者"
        icon={<IconUsers size={13} />}
        tone="c4"
        value={`${enabledAuthors}`}
        unit={`/ ${snapshot.authors.length}`}
      />
      {downloadTicks && downloadTicks.length >= 2 && (
        <div className="stat wide">
          <div className="label-row">
            <span className="chip c2"><IconDownload size={13} /></span>
            下载活动趋势（最近 {downloadTicks.length} 轮检查）
          </div>
          <div className="spark" style={{ marginTop: 4 }}>
            <Sparkline values={downloadTicks} height={44} />
          </div>
        </div>
      )}
    </div>
  )
}

export { useCountUp }
