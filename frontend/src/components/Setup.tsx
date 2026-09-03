import { useState } from 'react'
import { setupAuth } from '../api'

interface Props {
  onDone: () => void
}

/** 首次部署：强制设置访问密码 */
export default function Setup({ onDone }: Props) {
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const submit = async () => {
    if (busy) return
    if (pw.length < 4) {
      setErr('密码至少 4 位')
      return
    }
    if (pw !== pw2) {
      setErr('两次输入的密码不一致')
      return
    }
    setBusy(true)
    setErr('')
    try {
      await setupAuth(pw)
      onDone()
    } catch (e) {
      setErr(String(e).replace(/^Error:\s*/, ''))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form
        className="card login-box"
        onSubmit={(e) => { e.preventDefault(); submit() }}
      >
        <div className="login-brand">
          <img className="logo-img" src="/logo.png" alt="logo" />
          <div>
            <h1>媒体监控台</h1>
            <div className="sub">首次使用，请设置访问密码</div>
          </div>
        </div>
        <input
          className="input"
          type="password"
          placeholder="设置访问密码（至少 4 位）"
          value={pw}
          onChange={(e) => setPw(e.target.value)}
          autoFocus
          aria-label="设置访问密码"
        />
        <input
          className="input"
          type="password"
          placeholder="确认密码"
          value={pw2}
          onChange={(e) => setPw2(e.target.value)}
          aria-label="确认密码"
        />
        <button className="btn primary" type="submit" disabled={busy || !pw || !pw2}>
          {busy ? '保存中...' : '完成设置并进入'}
        </button>
        <div className="hint">
          设置后所有网页访问都需要先登录；之后可在「设置」页修改或停用鉴权。
        </div>
        {err && <div className="add-msg err" role="alert">{err}</div>}
      </form>
    </div>
  )
}
