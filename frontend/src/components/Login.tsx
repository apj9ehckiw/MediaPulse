import { useState } from 'react'
import { login } from '../api'

interface Props {
  onLogin: () => void
}

export default function Login({ onLogin }: Props) {
  const [pw, setPw] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const submit = async () => {
    if (!pw || busy) return
    setBusy(true)
    setErr('')
    try {
      await login(pw)
      onLogin()
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
          <span className="logo-img" aria-hidden="true">
            <svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
              <rect width="24" height="24" rx="6" fill="#2563eb" />
              <path d="M9.6 7.1v9.8a.55.55 0 0 0 .85.46l7.6-4.9a.55.55 0 0 0 0-.92l-7.6-4.9a.55.55 0 0 0-.85.46z" fill="#fff" />
            </svg>
          </span>
          <div>
            <h1>媒体监控台</h1>
            <div className="sub">请输入访问密码</div>
          </div>
        </div>
        <input
          className="input"
          type="password"
          placeholder="访问密码"
          value={pw}
          onChange={(e) => setPw(e.target.value)}
          autoFocus
          aria-label="访问密码"
        />
        <button className="btn primary" type="submit" disabled={busy || !pw}>
          {busy ? '登录中...' : '登录'}
        </button>
        {err && <div className="add-msg err" role="alert">{err}</div>}
      </form>
    </div>
  )
}
