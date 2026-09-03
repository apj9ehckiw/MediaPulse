import { useMemo, useState } from 'react'
import { VideoInfo, videoFileURL } from '../api'
import { IconFolder, IconPlay, IconSearch } from '../icons'
import { EmptyState } from './common'
import AuthorFilterBar from './AuthorFilterBar'

interface Props {
  videos: VideoInfo[]
  onPlay: (v: VideoInfo) => void
}

/** 缩略图：加载本地视频首帧（#t=0.1 + preload=metadata 只拉少量数据），失败回退占位底色 */
function Thumb({ v }: { v: VideoInfo }) {
  const [failed, setFailed] = useState(false)
  return (
    <div className="thumb">
      {!failed && (
        <video
          className="thumb-video"
          src={`${videoFileURL(v.file)}#t=0.1`}
          preload="metadata"
          muted
          playsInline
          tabIndex={-1}
          aria-hidden="true"
          onError={() => setFailed(true)}
        />
      )}
      <span className="play-ring"><IconPlay size={17} /></span>
      <span className="size-chip">
        {v.sizeMB >= 1024 ? `${(v.sizeMB / 1024).toFixed(2)} GB` : `${v.sizeMB.toFixed(1)} MB`}
      </span>
    </div>
  )
}

export default function VideoLibrary({ videos, onPlay }: Props) {
  const [query, setQuery] = useState('')
  const [authorFilter, setAuthorFilter] = useState<Set<number>>(new Set())

  // 作者筛选选项：按库内实际出现的作者聚合，附条数（按名称排序稳定顺序）
  const authorOptions = useMemo(() => {
    const counts = new Map<number, { name: string; count: number }>()
    for (const v of videos) {
      if (!v.authorUid) continue
      const name = v.authorName || String(v.authorUid)
      const cur = counts.get(v.authorUid) ?? { name, count: 0 }
      cur.count++
      counts.set(v.authorUid, cur)
    }
    return [...counts.entries()]
      .map(([uid, { name, count }]) => ({ uid, name, count }))
      .sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN'))
  }, [videos])

  // 已选作者的记录被清掉（清空去重记录）后自动剔除失效选择
  const visibleUids = useMemo(() => new Set(authorOptions.map((o) => o.uid)), [authorOptions])
  if (authorFilter.size > 0) {
    const alive = [...authorFilter].filter((uid) => visibleUids.has(uid))
    if (alive.length !== authorFilter.size) {
      setAuthorFilter(new Set(alive))
    }
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return videos.filter((v) => {
      if (authorFilter.size > 0 && !(v.authorUid && authorFilter.has(v.authorUid))) return false
      if (q && !(v.title || `topic_${v.topicId}`).toLowerCase().includes(q)) return false
      return true
    })
  }, [videos, query, authorFilter])

  const hasFilter = query.trim() !== '' || authorFilter.size > 0

  return (
    <div className="card">
      <div className="card-head">
        <h3>
          <span className="h-icon"><IconFolder size={14} /></span>
          视频库
        </h3>
        <div className="side">
          {videos.length > 0 && (
            <span className="badge">
              {hasFilter ? `${filtered.length} / ${videos.length} 个` : `${videos.length} 个`}
              {` · ${totalSize(filtered)}`}
            </span>
          )}
        </div>
      </div>

      {videos.length > 0 && (
        <div className="disc-toolbar">
          <div className="filter-row" role="group" aria-label="视频库筛选">
            <label className="lib-search">
              <IconSearch size={13} />
              <input
                className="input"
                type="text"
                placeholder="搜索标题"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
            <AuthorFilterBar
              options={authorOptions}
              selected={authorFilter}
              onChange={setAuthorFilter}
            />
            {hasFilter && (
              <button
                className="btn ghost"
                onClick={() => { setQuery(''); setAuthorFilter(new Set()) }}
              >
                重置
              </button>
            )}
          </div>
        </div>
      )}

      {videos.length === 0 ? (
        <EmptyState
          title="视频库还是空的"
          hint="监控发现新视频并下载完成后会出现在这里"
          icon={<IconFolder size={26} strokeWidth={1.5} />}
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          title="没有匹配的视频"
          hint="调整搜索关键词或作者筛选条件"
        />
      ) : (
        <div className="video-grid">
          {filtered.map((v) => (
            <div className="video-card" key={v.topicId} onClick={() => onPlay(v)} title="点击播放">
              <Thumb v={v} />
              <div className="v-body">
                <div className="v-title">{v.title || `topic_${v.topicId}`}</div>
                <div className="v-meta two-line">
                  <div className="v-meta-row">
                    {v.authorUid ? <span className="a-chip">作者 {v.authorName || v.authorUid}</span> : null}
                  </div>
                  <div className="v-meta-row">
                    <span>{v.createTime?.slice(0, 10) ?? '—'}</span>
                    <span>·</span>
                    <span>{v.doneAt.slice(5, 16)} 下载</span>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/** 视频库总大小（卡片头部徽标，随筛选联动） */
function totalSize(videos: VideoInfo[]): string {
  const mb = videos.reduce((s, v) => s + v.sizeMB, 0)
  return mb >= 1024 ? `${(mb / 1024).toFixed(2)} GB` : `${mb.toFixed(1)} MB`
}

export { videoFileURL }
