import { useState } from 'react'
import { addAuthorRemote, removeAuthorRemote, setAuthorEnabled, AuthorStat, Snapshot } from '../api'
import { IconClose, IconPlus, IconUsers } from '../icons'
import { EmptyState } from './common'

interface Props {
  snapshot: Snapshot | null
  /** 增删改作者后通知 App 立即刷新快照 */
  onRefresh: () => void
}

export default function Authors({ snapshot, onRefresh }: Props) {
  const [uidText, setUidText] = useState('')
  const [busy, setBusy] = useState(false)
  const [busyUid, setBusyUid] = useState<number | null>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [removing, setRemoving] = useState<AuthorStat | null>(null)

  const submit = async () => {
    const uid = Number(uidText.trim())
    if (!uid || uid <= 0) {
      setMsg({ ok: false, text: '请输入有效的作者 UID（作者主页 /homepage/<UID> 中的数字）' })
      return
    }
    setBusy(true)
    setMsg(null)
    try {
      const { name } = await addAuthorRemote(uid)
      setMsg({ ok: true, text: `已添加：${name}（${uid}），正在自动检查该作者的全部帖子` })
      setUidText('')
      onRefresh()
    } catch (e) {
      setMsg({ ok: false, text: String(e) })
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (uid: number, enabled: boolean) => {
    setBusyUid(uid)
    setMsg(null)
    try {
      await setAuthorEnabled(uid, enabled)
      setMsg({ ok: true, text: `已${enabled ? '开启' : '关闭'}作者 ${uid} 的监控` })
      onRefresh()
    } catch (e) {
      setMsg({ ok: false, text: String(e) })
    } finally {
      setBusyUid(null)
    }
  }

  const authors = snapshot?.authors ?? []

  return (
    <div className="settings-wrap page">
      <div className="card">
        <div className="card-head">
          <h3>
            <span className="h-icon"><IconPlus size={14} /></span>
            添加作者
          </h3>
        </div>
        <div className="settings-body">
          <div className="add-form">
            <input
              className="input uid"
              type="number"
              placeholder="作者 UID"
              value={uidText}
              onChange={(e) => setUidText(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !busy) submit() }}
              aria-label="作者 UID"
            />
            <button className="btn primary" onClick={submit} disabled={busy}>
              {busy ? '获取名字中...' : '自动获取名字并添加'}
            </button>
          </div>
          {msg && (
            <div className={`add-msg ${msg.ok ? 'ok' : 'err'}`} role={msg.ok ? 'status' : 'alert'}>
              {msg.text}
            </div>
          )}
          <div className="hint">
            只需填写作者 UID，名字会从站点自动获取。添加后立即增量检查该作者，
            新发现的视频进入「发现」页等待手动下载。
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <h3>
            <span className="h-icon"><IconUsers size={14} /></span>
            作者管理
          </h3>
          <div className="side">
            <span className="badge">
              {authors.filter((a) => a.enabled).length} / {authors.length} 监控中
            </span>
          </div>
        </div>
        {authors.length === 0 ? (
          <EmptyState title="还没有监控的作者" hint="在上方输入 UID 添加第一个作者" />
        ) : (
          <div className="author-list">
            {authors.map((a) => (
              <div className={`author-card-row ${a.enabled ? '' : 'off'}`} key={a.uid}>
                <div className="a-main">
                  <div className="a-name">
                    {a.note || `作者 ${a.uid}`}
                    {a.checking && (
                      <span className="a-checking" title={a.checkInfo}>
                        <span className="spin-dot" />
                        {a.checkInfo || '检测中'}
                      </span>
                    )}
                  </div>
                  <div className="a-uid">UID {a.uid}</div>
                </div>
                <label className="switch-label" title={a.enabled ? '点击停止监控该作者' : '点击开始监控该作者'}>
                  <input
                    type="checkbox"
                    checked={a.enabled}
                    disabled={busyUid === a.uid}
                    onChange={(e) => toggle(a.uid, e.target.checked)}
                  />
                  {a.enabled ? '监控中' : '已停用'}
                </label>
                <div className="a-stats" aria-label={`作者 ${a.note || a.uid} 的检查状态`}>
                  <span>视频帖 <b>{a.videos}</b></span>
                  <span>已下载 <b>{a.downloaded}</b></span>
                  <span className={a.pending > 0 ? 'a-pending' : ''}>待处理 <b>{a.pending}</b></span>
                </div>
                <div className="a-last">
                  {a.checking ? (
                    <span className="a-progress-hint">{a.checkInfo}</span>
                  ) : a.lastCheck ? `检查于 ${a.lastCheck}` : '尚未检查'}
                </div>
                <div className="a-actions">
                  <button
                    className="btn ghost danger-ghost remove-wipe"
                    onClick={() => setRemoving(a)}
                    disabled={busyUid === a.uid}
                    title="删除作者（可选同时删除视频与记录）"
                  >
                    删除
                  </button>
                </div>
              </div>
            ))}
            <div className="hint" style={{ padding: '10px 16px' }}>
              「待处理」= 检测到但尚未成功下载的视频帖（含下载失败可重试的），
              与「已下载」相加即为该作者当前检测到的全部视频帖。
              删除作者时可选择是否同时删除其视频文件与相关记录。
            </div>
          </div>
        )}
      </div>

      {removing && (
        <RemoveDialog
          author={removing}
          onClose={() => setRemoving(null)}
          onDone={(text) => {
            setRemoving(null)
            setMsg({ ok: true, text })
            onRefresh()
          }}
        />
      )}
    </div>
  )
}

/** 删除作者对话框：复选框选择是否连带删除视频/记录 */
function RemoveDialog({ author, onClose, onDone }: {
  author: AuthorStat
  onClose: () => void
  onDone: (msg: string) => void
}) {
  const [delVideos, setDelVideos] = useState(false)
  const [delRecords, setDelRecords] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const confirm = async () => {
    if (busy) return
    setBusy(true)
    setErr('')
    try {
      await removeAuthorRemote(author.uid, delVideos, delRecords)
      onDone(delVideos || delRecords
        ? `已删除作者 ${author.note || author.uid}${delVideos ? ' 及其视频文件' : ''}${delRecords ? ' 及相关记录' : ''}`
        : `已删除作者 ${author.note || author.uid}（视频与记录保留）`)
    } catch (e) {
      setErr(String(e))
      setBusy(false)
    }
  }

  return (
    <div className="modal-overlay" onClick={busy ? undefined : onClose}>
      <div className="modal-box remove-box" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <span className="t">删除作者「{author.note || author.uid}」</span>
          <button className="btn ghost icon-only" onClick={onClose} disabled={busy} title="关闭">
            <IconClose size={14} />
          </button>
        </div>
        <div className="remove-body">
          <div className="remove-tip">
            从监控列表移除该作者（UID {author.uid}）。
            已下载 <b>{author.downloaded}</b> 个视频 · 待处理 <b>{author.pending}</b> 条。
          </div>
          <label className="remove-option">
            <input
              type="checkbox"
              checked={delVideos}
              onChange={(e) => setDelVideos(e.target.checked)}
            />
            <span className="opt-main">同时删除视频文件</span>
            <span className="opt-sub">videos/ 下该作者的整个文件夹（不可恢复）</span>
          </label>
          <label className="remove-option">
            <input
              type="checkbox"
              checked={delRecords}
              onChange={(e) => setDelRecords(e.target.checked)}
            />
            <span className="opt-main">同时删除相关记录</span>
            <span className="opt-sub">下载去重 + 发现记录 + 下载流水，并取消进行中的任务；重新添加后会重新发现并下载</span>
          </label>
          <div className="remove-warn">
            {(delVideos || delRecords) && '⚠ 勾选的删除操作不可恢复，请确认。'}
            {!delVideos && !delRecords && '未勾选任何选项：仅从列表移除，之后重新添加同 UID 可继续。'}
          </div>
          {err && <div className="add-msg err" role="alert">{err}</div>}
        </div>
        <div className="modal-foot remove-foot">
          <button className="btn ghost" onClick={onClose} disabled={busy}>取消</button>
          <button
            className={`btn ${(delVideos || delRecords) ? 'primary' : 'ghost'}`}
            onClick={confirm}
            disabled={busy}
          >
            {busy ? '删除中...' : '确认删除'}
          </button>
        </div>
      </div>
    </div>
  )
}
