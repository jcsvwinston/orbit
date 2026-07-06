// Shared redesign primitives (design handoff "Orbit Admin"). Every color is a
// token class from tailwind.config.js, so both themes resolve at runtime.
import { type ReactNode } from 'react'

/** Semantic stream/status colors, resolved per theme via tokens. */
export const SEMANTIC = {
  blue: 'var(--t48)',
  green: 'var(--t49)',
  amber: 'var(--t50)',
  red: 'var(--t51)',
  violet: 'var(--t52)',
  accent: 'var(--accent)',
} as const

export type SemanticColor = keyof typeof SEMANTIC

export function methodColor(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
      return SEMANTIC.blue
    case 'POST':
      return SEMANTIC.green
    case 'PUT':
    case 'PATCH':
      return SEMANTIC.amber
    case 'DELETE':
      return SEMANTIC.red
    default:
      return SEMANTIC.violet
  }
}

export function sqlKindColor(op: string): string {
  switch (op.toUpperCase()) {
    case 'SELECT':
      return SEMANTIC.blue
    case 'INSERT':
      return SEMANTIC.green
    case 'UPDATE':
      return SEMANTIC.amber
    case 'DELETE':
      return SEMANTIC.red
    default:
      return SEMANTIC.violet
  }
}

export function statusColor(status: number): string {
  if (status >= 500) return SEMANTIC.red
  if (status >= 400) return SEMANTIC.amber
  if (status >= 300) return SEMANTIC.blue
  return SEMANTIC.green
}

/** Pulsing dot (pulse keyframes live in index.css). */
export function Dot(props: { color: string; size?: number; pulse?: boolean }) {
  const s = props.size ?? 7
  return (
    <span
      className={props.pulse ? 'animate-pulse-dot' : undefined}
      style={{
        width: s,
        height: s,
        borderRadius: 99,
        background: props.color,
        display: 'inline-block',
        flexShrink: 0,
      }}
    />
  )
}

/** Bordered pill with a tinted background, e.g. Live / Paused / Healthy. */
export function Pill(props: { color: string; pulse?: boolean; children: ReactNode }) {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full border px-2.5 py-[3px] text-[11px]"
      style={{
        color: props.color,
        borderColor: `color-mix(in srgb, ${props.color} 28%, transparent)`,
        background: `color-mix(in srgb, ${props.color} 10%, transparent)`,
      }}
    >
      <span
        className={props.pulse ? 'animate-pulse-fast' : undefined}
        style={{ width: 6, height: 6, borderRadius: 99, background: props.color }}
      />
      {props.children}
    </span>
  )
}

/** Flat card: bg t5, border t18, radius 10 (no shadow). */
export function Card(props: { className?: string; children: ReactNode }) {
  return (
    <div className={['rounded-[10px] border border-t18 bg-t5', props.className ?? ''].join(' ')}>
      {props.children}
    </div>
  )
}

/** Card section title: 12.5px/600. */
export function CardTitle(props: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 pb-2 pt-3.5">
      <div className="text-[12.5px] font-semibold text-t41">{props.children}</div>
      {props.right}
    </div>
  )
}

/** 10px uppercase muted label. */
export function Label(props: { children: ReactNode; className?: string }) {
  return (
    <div
      className={[
        'text-[10px] font-semibold uppercase tracking-[.08em] text-t30',
        props.className ?? '',
      ].join(' ')}
    >
      {props.children}
    </div>
  )
}

/** Ghost button (bg t8, border t20). */
export function GhostButton(props: {
  onClick?: () => void
  danger?: boolean
  disabled?: boolean
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      disabled={props.disabled}
      className={[
        'rounded-[7px] border px-2.5 py-1 text-[12px] transition-colors disabled:cursor-not-allowed disabled:opacity-40',
        props.danger
          ? 'border-t51/40 bg-transparent text-t51 hover:bg-t51/10'
          : 'border-t20 bg-t8 text-t39 hover:bg-t9 hover:text-t45',
      ].join(' ')}
      style={props.danger ? { borderColor: 'color-mix(in srgb, var(--t51) 40%, transparent)' } : undefined}
    >
      {props.children}
    </button>
  )
}

/** Primary accent button (accent bg, dark text). */
export function AccentButton(props: { onClick?: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      disabled={props.disabled}
      className="rounded-[7px] px-3 py-1.5 text-[12.5px] font-semibold text-t53 transition-[filter] hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-40"
      style={{ background: 'var(--accent)' }}
    >
      {props.children}
    </button>
  )
}

/** Mono chip (labels, flags): bg t13, radius 4. */
export function Chip(props: { children: ReactNode; color?: string }) {
  return (
    <span
      className="rounded-[4px] bg-t13 px-1.5 py-0.5 font-mono text-[10.5px] text-t35"
      style={props.color ? { color: props.color, background: `color-mix(in srgb, ${props.color} 12%, transparent)` } : undefined}
    >
      {props.children}
    </span>
  )
}

/** Segmented control (mono, pill container). */
export function Segmented(props: {
  options: ReadonlyArray<{ id: string; label: string }>
  value: string
  onChange: (id: string) => void
}) {
  return (
    <div className="inline-flex items-center gap-0.5 rounded-[8px] border border-t18 bg-t5 p-0.5 font-mono text-[11.5px]">
      {props.options.map((o) => (
        <button
          key={o.id}
          type="button"
          onClick={() => props.onChange(o.id)}
          className={[
            'rounded-[6px] px-2.5 py-1 transition-colors',
            o.id === props.value ? 'bg-t17 text-t45' : 'text-t32 hover:text-t41',
          ].join(' ')}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/** Thin progress bar (CPU / pool usage). */
export function Progress(props: { pct: number; color?: string; height?: number }) {
  const pct = Math.max(0, Math.min(100, props.pct))
  return (
    <div
      className="w-full overflow-hidden rounded-full bg-t15"
      style={{ height: props.height ?? 4 }}
    >
      <div
        className="h-full rounded-full transition-[width]"
        style={{ width: `${pct}%`, background: props.color ?? 'var(--accent)' }}
      />
    </div>
  )
}
