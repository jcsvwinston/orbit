// Stream toolbar (design handoff): Live/Paused pill with pulsing dot,
// Pause/Resume + Clear ghost buttons, mono "N buffered" counter. Same public
// interface as before; only the visual language changed to the token system.
import { type ReactNode } from 'react'
import { GhostButton, Pill } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'

export interface StreamControlsProps {
  connected: boolean
  paused: boolean
  onTogglePause: () => void
  onClear: () => void
  count: number
  error?: string | null
  // pendingCount: events buffered while paused (shown on the Resume
  // button so the operator knows resuming reveals N new rows).
  pendingCount?: number
  extra?: ReactNode
}

export function StreamControls(props: StreamControlsProps) {
  const pill = !props.connected
    ? { color: SEMANTIC.red, label: t.stream.disconnected, pulse: false }
    : props.paused
      ? { color: SEMANTIC.amber, label: t.stream.paused, pulse: false }
      : { color: SEMANTIC.green, label: t.stream.live, pulse: true }

  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <Pill color={pill.color} pulse={pill.pulse}>
        {pill.label}
      </Pill>
      <GhostButton onClick={props.onTogglePause}>
        {props.paused
          ? props.pendingCount && props.pendingCount > 0
            ? t.stream.resumePending(props.pendingCount)
            : t.stream.resume
          : t.stream.pause}
      </GhostButton>
      <GhostButton onClick={props.onClear}>{t.common.clear}</GhostButton>
      <span className="font-mono text-[10.5px] text-t31">
        {t.stream.buffered(props.count)}
      </span>
      {props.error !== undefined && props.error !== null && (
        <span className="text-[11px] text-t51" title={props.error}>
          {props.error.slice(0, 80)}
        </span>
      )}
      {props.extra}
    </div>
  )
}
