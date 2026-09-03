import { useEffect, useState } from 'react'
import { AppConfig, fetchConfig, fetchFFmpeg, FFmpegStatus, installFFmpeg, updateConfig } from '../api'
import { IconSettings } from '../icons'

interface Props {
  onSaved: (msg: string) => void
}

const FF_TEXT: Record<FFmpegStatus['state'], string> = {
  checking: '检测中...',
  ready: '已就绪',
  installing: '下载安装中...',
  failed: '安装失败',
  missing: '未检测到',
}

export default function Settings({ onSaved }: Props) {
  const [cfg, setCfg] = useState<AppConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [ff, setFf] = useState<FFmpegStatus | null>(null)
  const [ffBusy, setFfBusy] = useState(false)

  useEffect(() => {
    fetchConfig()
      .then(setCfg)
      .catch((e) => setErr(String(e)))
    refreshFF()
  }, [])

  const refreshFF = () => {
    fetchFFmpeg().then(setFf).catch(() => {})
  }

  // 触发安装后轮询状态直到结束（最长 15 分钟；安装中 1s 轮询让进度及时）
  const installFF = async () => {
    setFfBusy(true)
    try {
      await installFFmpeg()
      const deadline = Date.now() + 15 * 60 * 1000
      const timer = setInterval(async () => {
        try {
          const st = await fetchFFmpeg()
          setFf(st)
          if (st.state !== 'installing' || Date.now() > deadline) {
            clearInterval(timer)
            setFfBusy(false)
          }
        } catch {
          clearInterval(timer)
          setFfBusy(false)
        }
      }, 1000)
    } catch (e) {
      setErr(String(e))
      setFfBusy(false)
    }
  }

  if (!cfg) {
    return (
      <div className="card">
        <div className="empty">{err || '加载配置...'}</div>
      </div>
    )
  }

  const save = async () => {
    setSaving(true)
    setErr('')
    try {
      const saved = await updateConfig(cfg)
      setCfg(saved)
      onSaved(`配置已保存：间隔 ${saved.intervalSec}s，并发 ${saved.workers}`)
    } catch (e) {
      setErr(String(e))
    } finally {
      setSaving(false)
    }
  }

  // 自动下载时间下限快捷选项：今天 / 昨天 / 近7天 / 近30天
  const afterPresets: { label: string; days: number }[] = [
    { label: '今天', days: 0 },
    { label: '昨天', days: 1 },
    { label: '近7天', days: 7 },
    { label: '近30天', days: 30 },
  ]
  const setAfterDays = (days: number) => {
    const d = new Date(Date.now() - days * 86400000)
    const p = (n: number) => String(n).padStart(2, '0')
    setCfg({ ...cfg, autoDownloadAfter: `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}` })
  }
  const matchesPreset = (days: number) => {
    const d = new Date(Date.now() - days * 86400000)
    const p = (n: number) => String(n).padStart(2, '0')
    return cfg.autoDownloadAfter === `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
  }

  return (
    <div className="settings-wrap page">
      {err && (
        <div className="card">
          <div className="empty" style={{ color: 'var(--err)' }}>{err}</div>
        </div>
      )}

      <div className="card">
        <div className="card-head">
          <h3>
            <span className="h-icon"><IconSettings size={14} /></span>
            下载设置
          </h3>
        </div>
        <div className="settings-body">
          <div className="form-row">
            <label>自动下载</label>
            <label className="switch-label">
              <input
                type="checkbox"
                checked={cfg.autoDownload}
                onChange={(e) => setCfg({ ...cfg, autoDownload: e.target.checked })}
              />
              {cfg.autoDownload ? '发现新视频后自动下载' : '关闭：新视频仅进入「发现」页'}
            </label>
          </div>
          {cfg.autoDownload && (
            <div className="form-row">
              <label>仅下载该日期后发布</label>
              <div className="auto-after">
                <input
                  className="input"
                  type="date"
                  value={cfg.autoDownloadAfter ?? ''}
                  onChange={(e) => setCfg({ ...cfg, autoDownloadAfter: e.target.value })}
                  aria-label="自动下载发布时间下限"
                />
                {afterPresets.map((p) => (
                  <button
                    key={p.label}
                    className={`chip-btn ${matchesPreset(p.days) ? 'active' : ''}`}
                    onClick={() => setAfterDays(p.days)}
                    type="button"
                  >
                    {p.label}
                  </button>
                ))}
                {cfg.autoDownloadAfter && (
                  <button
                    className="btn ghost"
                    type="button"
                    onClick={() => setCfg({ ...cfg, autoDownloadAfter: '' })}
                  >
                    不限制
                  </button>
                )}
              </div>
            </div>
          )}
          <div className="hint">
            {cfg.autoDownload
              ? cfg.autoDownloadAfter
                ? `只自动下载发布时间在 ${cfg.autoDownloadAfter} 之后的视频帖；更早的帖子会登记到「发现」页，可手动选择下载。「发现」页的手动下载不受此限制。`
                : '自动下载已开启：所有新发现的视频帖都会自动下载（不限制发布时间）。'
              : '自动下载默认关闭。关闭时，监控检查到的新视频只登记在「发现」页，你可以按发布时间筛选后手动选择下载。'}
          </div>
          <div className="form-row">
            <label>站点基址</label>
            <input
              className="input"
              value={cfg.apiBase}
              onChange={(e) => setCfg({ ...cfg, apiBase: e.target.value })}
            />
          </div>
          <div className="form-row">
            <label>轮询间隔（秒）</label>
            <input
              className="input"
              type="number"
              min={0}
              step={60}
              value={cfg.intervalSec}
              onChange={(e) => setCfg({ ...cfg, intervalSec: Number(e.target.value) || 0 })}
            />
            <span className="hint-inline">0 = 仅手动检查；最低 60s</span>
          </div>
          <div className="form-row">
            <label>列表类型</label>
            <select
              className="input"
              value={cfg.listType}
              onChange={(e) => setCfg({ ...cfg, listType: Number(e.target.value) })}
            >
              <option value={0}>全部帖子</option>
              <option value={1}>最新（默认）</option>
              <option value={3}>精华</option>
            </select>
          </div>
          <div className="form-row">
            <label>段下载并发</label>
            <input
              className="input"
              type="number"
              min={1}
              max={32}
              value={cfg.workers}
              onChange={(e) => setCfg({ ...cfg, workers: Number(e.target.value) || 8 })}
            />
            <span className="hint-inline">1–32，越高下载越快但更占带宽</span>
          </div>
          <div className="form-row">
            <label>ffmpeg</label>
            <span className="ff-state" title={ff?.path || ff?.error || ''}>
              {ff ? (
                <>
                  <span className={`dot ${ff.state === 'ready' ? 'ok-dot' : ff.state === 'failed' ? 'err-dot' : ''}`} />
                  {FF_TEXT[ff.state] ?? ff.state}
                  <span className="hint-inline">{ff.goos}/{ff.goarch}{ff.state === 'ready' && ff.path ? ` · ${ff.path}` : ff.error ? ` · ${ff.error}` : ''}</span>
                </>
              ) : '检测中...'}
            </span>
            <button className="btn ghost" onClick={installFF} disabled={ffBusy || ff?.state === 'installing'}>
              {ff?.state === 'ready' ? '重新检测' : ff?.state === 'missing' ? '下载并安装' : '检测 / 安装'}
            </button>
          </div>
          {ff?.state === 'installing' && <FFProgress ff={ff} />}
          {ff?.state === 'missing' && (
            <div className="ff-banner" role="alert">
              <b>未检测到 ffmpeg</b>
              <span>视频下载完成后需 ffmpeg 封装为 MP4。点击右侧「下载并安装」（约 80–130 MB，
                下载进度实时显示），或自行安装到 PATH 后点「重新检测」。</span>
            </div>
          )}
          <div className="form-row">
            <label>GitHub 加速代理</label>
            <input
              className="input"
              type="text"
              placeholder="如 https://ghproxy.net/（留空 = 直连）"
              value={cfg.githubProxy ?? ''}
              onChange={(e) => setCfg({ ...cfg, githubProxy: e.target.value })}
            />
          </div>
          <div className="hint">
            ffmpeg 安装包（Windows/macOS）从 GitHub 下载，国内直连较慢时可填写加速代理前缀，
            用法 <code>&lt;代理前缀&gt;https://github.com/...</code>（如 ghproxy.net、mirror.ghproxy.com）。
            仅影响 ffmpeg 安装包下载，视频下载不走此代理。
          </div>
          <div className="form-row">
            <label>ffmpeg 自动安装</label>
            <label className="switch-label">
              <input
                type="checkbox"
                checked={!!cfg.ffmpegAutoInstall}
                onChange={(e) => setCfg({ ...cfg, ffmpegAutoInstall: e.target.checked })}
              />
              {cfg.ffmpegAutoInstall ? '缺失时自动下载安装' : '关闭（默认）：缺失时仅提示'}
            </label>
          </div>
          <div className="hint">
            默认不自动下载：检测到 ffmpeg 缺失时仅在设置页提示，由你手动点击「下载并安装」；
            开启后恢复启动时自动安装。安装位置为数据目录 bin/。
          </div>
          <div className="form-row">
            <label>访问密码</label>
            <input
              className="input"
              type="password"
              placeholder={cfg.hasPassword ? '已设置（输入新密码可修改）' : '未设置 = 无鉴权（至少 4 位）'}
              value={cfg.password ?? ''}
              onChange={(e) => setCfg({ ...cfg, password: e.target.value })}
              autoComplete="new-password"
            />
            {cfg.hasPassword && (
              <label className="switch-label">
                <input
                  type="checkbox"
                  checked={!!cfg.clearPassword}
                  onChange={(e) => setCfg({ ...cfg, clearPassword: e.target.checked })}
                />
                停用鉴权
              </label>
            )}
          </div>
          <div className="hint">
            设置密码后，所有网页访问都需要先登录；密码修改后已登录的会话全部失效。
            留空表示保持当前密码不变；勾选「停用鉴权」后任何人可直接访问，
            之后再想启用，重新设置一个新密码即可。
          </div>
          <div className="save-row">
            <button className="btn primary" onClick={save} disabled={saving}>
              {saving ? '保存中...' : '保存配置'}
            </button>
          </div>
          <div className="hint">
            监控哪些作者在左侧「作者」页管理：添加、启用/停用、删除。
          </div>
        </div>
      </div>
    </div>
  )
}

/** ffmpeg 安装包下载进度条（installing 状态时显示） */
function FFProgress({ ff }: { ff: FFmpegStatus }) {
  const done = ff.progressDone ?? -1
  const total = ff.progressTotal ?? -1
  const known = done >= 0 && total > 0
  const pct = known ? Math.min(Math.round((done / total) * 100), 100) : 0
  return (
    <div className="ff-progress" role="status" aria-live="polite">
      <div className="ff-progress-text">
        下载安装中…
        {done >= 0 && (
          <span className="ff-progress-detail">
            {known
              ? `${fmtMB(done)} / ${fmtMB(total)}（${pct}%）`
              : `${fmtMB(done)}（大小未知）`}
          </span>
        )}
      </div>
      {known ? (
        <div className="progressbar" style={{ height: 8 }}>
          <div className="fill" style={{ width: `${Math.max(pct, 3)}%` }} />
        </div>
      ) : (
        <div className="progressbar indeterminate" style={{ height: 8 }}>
          <div className="fill" style={{ width: '34%' }} />
        </div>
      )}
    </div>
  )
}

function fmtMB(bytes: number): string {
  return bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MB` : `${(bytes / 1024).toFixed(0)} KB`
}
