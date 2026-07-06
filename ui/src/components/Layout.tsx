// Redesigned shell (design handoff "Orbit Admin"): fixed 212px sidebar with
// grouped navigation (Observe / Fleet / Manage), orbit glyph, footer with the
// reachability dot + identity line and theme toggle, independently scrolling
// main area. PageHeader/PageBody moved to components/Page.tsx.
import { type ReactNode } from 'react'

export interface NavItem {
  id: string
  label: string
  badge?: number
}

export interface NavGroup {
  name: string
  items: NavItem[]
}

export type ThemeName = 'light' | 'dark'

export interface LayoutProps {
  current: string
  groups: NavGroup[]
  onNavigate: (id: string) => void
  serverHealthy: boolean
  version: string
  identity?: string
  theme: ThemeName
  onToggleTheme: () => void
  children: ReactNode
}

function OrbitGlyph() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" className="shrink-0" aria-hidden>
      <ellipse
        cx="12"
        cy="12"
        rx="10"
        ry="4.4"
        fill="none"
        stroke="var(--t21)"
        strokeWidth="1.2"
        transform="rotate(-24 12 12)"
      />
      <circle cx="12" cy="12" r="4.2" fill="var(--accent)" />
      <circle cx="3.6" cy="15.2" r="1.5" fill="var(--t40)" />
    </svg>
  )
}

export function Layout(props: LayoutProps) {
  return (
    <div className="flex min-h-screen bg-t0 font-sans text-t46">
      <aside className="sticky top-0 flex h-screen w-[212px] shrink-0 flex-col border-r border-t14 bg-t1">
        <div className="flex items-center gap-2.5 px-4 pb-2.5 pt-[18px]">
          <OrbitGlyph />
          <div>
            <div className="text-[13.5px] font-[650] tracking-[.01em] text-t46">Orbit</div>
            <div className="text-[10px] uppercase tracking-[.09em] text-t27">Nucleus admin</div>
          </div>
        </div>
        <nav className="flex-1 overflow-y-auto px-2.5 pb-2.5">
          {props.groups.map((g) => (
            <div key={g.name} className="mt-3.5">
              <div className="px-2.5 pb-[5px] text-[10px] font-semibold uppercase tracking-[.11em] text-t24">
                {g.name}
              </div>
              <div className="flex flex-col gap-px">
                {g.items.map((it) => {
                  const active = it.id === props.current
                  return (
                    <button
                      key={it.id}
                      type="button"
                      onClick={() => props.onNavigate(it.id)}
                      className={[
                        'flex w-full items-center justify-between gap-2 rounded-[7px] px-2.5 py-[6.5px] text-left text-[13px] transition-colors',
                        active ? 'bg-t13 text-t47' : 'text-t33 hover:bg-t9 hover:text-t45',
                      ].join(' ')}
                    >
                      <span className="flex items-center gap-[9px]">
                        <span
                          className="h-[13px] w-[3px] shrink-0 rounded-[2px]"
                          style={{ background: active ? 'var(--accent)' : 'transparent' }}
                        />
                        {it.label}
                      </span>
                      {it.badge !== undefined && (
                        <span className="rounded-full bg-t16 px-[7px] py-px font-mono text-[10.5px] text-t35">
                          {it.badge}
                        </span>
                      )}
                    </button>
                  )
                })}
              </div>
            </div>
          ))}
        </nav>
        <div className="flex flex-col gap-1.5 border-t border-t10 px-4 py-3">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-[7px] text-[11.5px] text-t32">
              <span
                className="animate-pulse-dot inline-block h-[7px] w-[7px] rounded-full"
                style={{ background: props.serverHealthy ? 'var(--t49)' : 'var(--t51)' }}
              />
              {props.serverHealthy ? 'Server reachable' : 'Server unreachable'}
            </div>
            <button
              type="button"
              onClick={props.onToggleTheme}
              title={props.theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
              className="rounded-[6px] border border-t20 bg-t8 px-1.5 py-0.5 font-mono text-[10px] text-t32 transition-colors hover:text-t45"
            >
              {props.theme === 'dark' ? '☾' : '☀'}
            </button>
          </div>
          <div className="font-mono text-[10.5px] text-t22">
            {props.version}
            {props.identity ? ` · ${props.identity}` : ''}
          </div>
        </div>
      </aside>
      <main className="h-screen min-w-0 flex-1 overflow-y-auto">{props.children}</main>
    </div>
  )
}
