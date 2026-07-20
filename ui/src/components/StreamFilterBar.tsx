// StreamFilterBar renders the filter + sampling controls for a stream page.
// Presentational: all state lives in useStreamFilters. Which controls show
// depends on the kind (method/path/status for HTTP, model for SQL; node +
// sampling on all).
import { type ReactNode } from 'react'
import { GhostButton, Label } from '@/components/ui'
import { t } from '@/lib/i18n'
import type { FilterKind, StreamFilterState } from '@/hooks/useStreamFilters'
import type { NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'] as const
const STATUS_CLASSES: ReadonlyArray<{ id: string; label: string }> = [
  { id: '2', label: '2xx' },
  { id: '3', label: '3xx' },
  { id: '4', label: '4xx' },
  { id: '5', label: '5xx' },
]
const SAMPLING_OPTIONS = [100, 50, 10, 1] as const

export interface StreamFilterBarProps {
  kind: FilterKind
  state: StreamFilterState
  setState: (patch: Partial<StreamFilterState>) => void
  reset: () => void
  active: boolean
  nodes: NodeInfo[]
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value]
}

/** A small toggleable filter chip. */
function ToggleChip(props: { on: boolean; onClick: () => void; color?: string; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={props.on}
      onClick={props.onClick}
      className={[
        'rounded-[6px] border px-2 py-[3px] font-mono text-[11px] transition-colors',
        props.on ? 'border-transparent text-t53' : 'border-t20 bg-t8 text-t32 hover:text-t44',
      ].join(' ')}
      style={props.on ? { background: props.color ?? 'var(--accent)' } : undefined}
    >
      {props.children}
    </button>
  )
}

const inputClass =
  'rounded-[7px] border border-t19 bg-t8 px-2.5 py-[5.5px] font-mono text-[11.5px] text-t45 placeholder:text-t26 focus:outline-none'

export function StreamFilterBar(props: StreamFilterBarProps) {
  const { kind, state, setState, reset, active, nodes } = props
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-t14 px-7 py-2.5">
      {kind === 'http' && (
        <>
          <span className="flex items-center gap-1.5">
            <Label>{t.stream.filterMethod}</Label>
            {HTTP_METHODS.map((m) => (
              <ToggleChip
                key={m}
                on={state.methods.includes(m)}
                onClick={() => setState({ methods: toggle(state.methods, m) })}
              >
                {m}
              </ToggleChip>
            ))}
          </span>
          <span className="flex items-center gap-1.5">
            <Label>{t.stream.filterStatus}</Label>
            {STATUS_CLASSES.map((s) => (
              <ToggleChip
                key={s.id}
                on={state.statusClasses.includes(s.id)}
                onClick={() => setState({ statusClasses: toggle(state.statusClasses, s.id) })}
              >
                {s.label}
              </ToggleChip>
            ))}
          </span>
          <input
            type="text"
            value={state.pathGlob}
            onChange={(e) => setState({ pathGlob: e.target.value })}
            placeholder={t.stream.pathGlobPlaceholder}
            aria-label={t.stream.pathGlobAria}
            className={`${inputClass} w-[180px]`}
          />
        </>
      )}

      {kind === 'sql' && (
        <input
          type="text"
          value={state.sqlModel}
          onChange={(e) => setState({ sqlModel: e.target.value })}
          placeholder={t.stream.sqlModelPlaceholder}
          aria-label={t.stream.sqlModelAria}
          className={`${inputClass} w-[180px]`}
        />
      )}

      <span className="flex items-center gap-1.5">
        <Label>{t.stream.filterNode}</Label>
        <select
          value={state.nodeId}
          onChange={(e) => setState({ nodeId: e.target.value })}
          aria-label={t.stream.nodeFilterAria}
          className={`${inputClass} max-w-[160px]`}
        >
          <option value="">{t.stream.allNodes}</option>
          {nodes.map((n) => (
            <option key={n.nodeId} value={n.nodeId}>
              {n.nodeId}
            </option>
          ))}
        </select>
      </span>

      <span className="flex items-center gap-1.5">
        <Label>{t.stream.filterSample}</Label>
        <select
          value={state.samplingPct}
          onChange={(e) => setState({ samplingPct: Number(e.target.value) })}
          aria-label={t.stream.samplingAria}
          className={`${inputClass}`}
        >
          {SAMPLING_OPTIONS.map((pct) => (
            <option key={pct} value={pct}>
              {pct}%
            </option>
          ))}
        </select>
      </span>

      {active && (
        <GhostButton onClick={reset}>{t.stream.clearFilters}</GhostButton>
      )}
    </div>
  )
}
