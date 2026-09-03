import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { IconClose, IconSearch, IconUsers } from '../icons'

export interface AuthorFilterOption {
  uid: number
  name: string
  count: number
}

interface Props {
  /** 可选作者列表（含每个作者在当前发现记录中的条数） */
  options: AuthorFilterOption[]
  /** 已选中的作者 UID 集合；空 = 全部作者 */
  selected: Set<number>
  onChange: (next: Set<number>) => void
}

const PANEL_W = 288

interface PanelPos {
  top?: number
  bottom?: number
  left: number
  maxH?: number
}

/**
 * 作者筛选按钮 + 点击弹出的独立悬浮窗：按钮放在工具栏里随布局排布，
 * 悬浮窗通过 portal 挂到 body（不受卡片 overflow 裁剪），锚定在按钮下方；
 * 下方空间不足时翻折到按钮上方。支持同时勾选多个作者（并集筛选）。
 */
export default function AuthorFilterBar({ options, selected, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const btnRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<PanelPos | null>(null)

  // 定位：默认按钮正下方、左对齐且不越出视口；下方空间不足时翻到上方（面板底边贴按钮顶）
  const updatePos = () => {
    const b = btnRef.current?.getBoundingClientRect()
    if (!b) return
    const left = Math.max(12, Math.min(b.left, window.innerWidth - PANEL_W - 12))
    const spaceBelow = window.innerHeight - b.bottom - 12
    if (spaceBelow >= 300) {
      setPos({ top: b.bottom + 8, left })
    } else {
      setPos({ bottom: window.innerHeight - b.top + 8, left, maxH: Math.max(220, b.top - 20) })
    }
  }

  useLayoutEffect(() => {
    if (!open) return
    updatePos()
    window.addEventListener('resize', updatePos)
    window.addEventListener('scroll', updatePos, true)
    return () => {
      window.removeEventListener('resize', updatePos)
      window.removeEventListener('scroll', updatePos, true)
    }
  }, [open])

  // 点击按钮与悬浮窗之外 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (btnRef.current?.contains(t) || panelRef.current?.contains(t)) return
      setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  const toggle = (uid: number) => {
    const next = new Set(selected)
    if (next.has(uid)) next.delete(uid)
    else next.add(uid)
    onChange(next)
  }

  const selectAll = () => onChange(new Set(options.map((o) => o.uid)))
  const clearAll = () => onChange(new Set())

  const q = query.trim().toLowerCase()
  const visible = q
    ? options.filter((o) => o.name.toLowerCase().includes(q) || String(o.uid).includes(q))
    : options
  const allVisibleSelected = visible.length > 0 && visible.every((o) => selected.has(o.uid))
  const hasSel = selected.size > 0

  return (
    <div className="afb">
      <button
        ref={btnRef}
        type="button"
        className={`afb-trigger ${hasSel ? 'has-sel' : ''} ${open ? 'open' : ''}`}
        onClick={() => setOpen((v) => !v)}
        disabled={options.length === 0}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={options.length === 0 ? '暂无可筛选的作者' : '按作者筛选（可多选）'}
      >
        <IconUsers size={13} />
        <span>{hasSel ? `作者 · ${selected.size}` : '作者筛选'}</span>
        {hasSel && !open && (
          <span
            className="afb-clear"
            role="button"
            aria-label="清除作者筛选"
            title="清除作者筛选"
            onClick={(e) => { e.stopPropagation(); clearAll() }}
          >
            <IconClose size={11} />
          </span>
        )}
      </button>

      {open && pos && createPortal(
        <div
          className="afb-panel"
          ref={panelRef}
          role="dialog"
          aria-label="作者筛选（可多选）"
          style={{ top: pos.top, bottom: pos.bottom, left: pos.left, maxHeight: pos.maxH }}
        >
          <div className="afb-search">
            <IconSearch size={13} />
            <input
              className="input"
              type="text"
              placeholder="搜索作者 / UID"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              autoFocus
            />
          </div>

          <div className="afb-toolbar">
            <button className="btn ghost" onClick={allVisibleSelected ? clearAll : selectAll} disabled={options.length === 0}>
              {allVisibleSelected ? '取消全选' : '全选'}
            </button>
            <button className="btn ghost" onClick={clearAll} disabled={!hasSel}>
              清空
            </button>
            <span className="afb-toolbar-hint">{hasSel ? `已选 ${selected.size}/${options.length}` : `${options.length} 位作者`}</span>
          </div>

          <div className="afb-list" role="listbox" aria-multiselectable="true">
            {visible.length === 0 ? (
              <div className="afb-empty">{q ? '没有匹配的作者' : '暂无可筛选的作者'}</div>
            ) : (
              visible.map((o) => {
                const on = selected.has(o.uid)
                return (
                  <button
                    key={o.uid}
                    className={`afb-option ${on ? 'on' : ''}`}
                    role="option"
                    aria-selected={on}
                    onClick={() => toggle(o.uid)}
                  >
                    <span className="afb-check"><input type="checkbox" checked={on} readOnly tabIndex={-1} /></span>
                    <span className="afb-name">{o.name}</span>
                    <span className="afb-n">{o.count}</span>
                  </button>
                )
              })
            )}
          </div>

          {hasSel && (
            <div className="afb-selbar">
              {options.filter((o) => selected.has(o.uid)).map((o) => (
                <button key={o.uid} className="afb-tag" onClick={() => toggle(o.uid)} title={`移除 ${o.name}`}>
                  <span className="afb-name">{o.name}</span>
                  <IconClose size={10} />
                </button>
              ))}
            </div>
          )}
        </div>,
        document.body,
      )}
    </div>
  )
}
