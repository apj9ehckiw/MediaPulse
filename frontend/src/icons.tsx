// 极简线性图标集（stroke 风格，统一 24 viewBox / currentColor）

interface IconProps {
  size?: number
  strokeWidth?: number
}

const base = (p: IconProps) => ({
  width: p.size ?? 16,
  height: p.size ?? 16,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: p.strokeWidth ?? 1.9,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
})

export const IconDashboard = (p: IconProps) => (
  <svg {...base(p)}>
    <rect x="3" y="3" width="8" height="10" rx="2" />
    <rect x="13" y="3" width="8" height="6" rx="2" />
    <rect x="13" y="11" width="8" height="10" rx="2" />
    <rect x="3" y="15" width="8" height="6" rx="2" />
  </svg>
)

export const IconHistory = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M3 12a9 9 0 1 0 3-6.7L3 8" />
    <path d="M3 3v5h5" />
    <path d="M12 7v5l3.5 2" />
  </svg>
)

export const IconSettings = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="12" cy="12" r="3.2" />
    <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34h0a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55h0a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v0a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.55 1z" />
  </svg>
)

export const IconRefresh = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M21 12a9 9 0 1 1-2.64-6.36" />
    <path d="M21 3v6h-6" />
  </svg>
)

export const IconPlay = (p: IconProps) => (
  <svg {...base(p)} fill="currentColor" stroke="none">
    <path d="M8 5.5v13a.6.6 0 0 0 .92.5l10.3-6.5a.6.6 0 0 0 0-1L8.92 5a.6.6 0 0 0-.92.5z" />
  </svg>
)

export const IconVideo = (p: IconProps) => (
  <svg {...base(p)}>
    <rect x="2.5" y="5" width="15" height="14" rx="2.5" />
    <path d="m22 8-4.5 3v2L22 16V8z" />
  </svg>
)

export const IconDownload = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 3v12" />
    <path d="m7 11 5 5 5-5" />
    <path d="M4 20h16" />
  </svg>
)

export const IconUsers = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="9" cy="8" r="3.4" />
    <path d="M2.8 20a6.2 6.2 0 0 1 12.4 0" />
    <path d="M16.5 5.2a3.4 3.4 0 0 1 0 5.9" />
    <path d="M18 14.4a6.2 6.2 0 0 1 3.4 5.6" />
  </svg>
)

export const IconFolder = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M3 7a2 2 0 0 1 2-2h4l2.2 2.6H19a2 2 0 0 1 2 2V17a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" />
  </svg>
)

export const IconClock = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="12" cy="12" r="8.6" />
    <path d="M12 7.5V12l3 1.8" />
  </svg>
)

export const IconClose = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="m6 6 12 12M18 6 6 18" />
  </svg>
)

export const IconPlus = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M12 5v14M5 12h14" />
  </svg>
)

export const IconInbox = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M3 13.5 5.4 5a2 2 0 0 1 1.9-1.4h9.4a2 2 0 0 1 1.9 1.4L21 13.5V18a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 18v-4.5z" />
    <path d="M3 13.5h5l1.2 2.3h5.6l1.2-2.3h5" />
  </svg>
)

export const IconGlobe = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="12" cy="12" r="8.6" />
    <path d="M3.4 12h17.2" />
    <path d="M12 3.4c2.3 2.3 3.4 5.2 3.4 8.6s-1.1 6.3-3.4 8.6c-2.3-2.3-3.4-5.2-3.4-8.6s1.1-6.3 3.4-8.6z" />
  </svg>
)

export const IconSearch = (p: IconProps) => (
  <svg {...base(p)}>
    <circle cx="11" cy="11" r="7" />
    <path d="m20.5 20.5-4.6-4.6" />
  </svg>
)

export const IconLogout = (p: IconProps) => (
  <svg {...base(p)}>
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
    <path d="m16 17 5-5-5-5" />
    <path d="M21 12H9" />
  </svg>
)
