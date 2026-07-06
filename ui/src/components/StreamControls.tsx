// Stream toolbar (design handoff): Live/Paused pill with pulsing dot,
// Pause/Resume + Clear ghost buttons, mono "N buffered" counter. Same public
// interface as before; only the visual language changed to the token system.
import { type ReactNode } from 'react'
import { GhostButton, Pill, SEMANTIC } from '@/components/ui'

export interface StreamControlsProps {
  connected: boolean
  paused: boolean
  onTogglePause: () => void
  onClear: () => void
  count: number
  error?: string | null
  extra?: ReactNode
}

export function StreamControls(props: StreamControlsProps) {
  const pill = !props.connected
    ? { color: SEMANTIC.red, label: 'Disconnected', pulse: false }
    : props.paused
      ? { color: SEMANTIC.amber, label: 'Paused', pulse: false }
      : { color: SEMANTIC.green, label: 'Live', pulse: true }

  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <Pill color={pill.color} pulse={pill.pulse}>
        {pill.label}
      </Pill>
      <GhostButton onClick={props.onTogglePause}>{props.paused ? 'Resume' : 'Pause'}</GhostButton>
      <GhostButton onClick={props.onClear}>Clear</GhostButton>
      <span className="font-mono text-[10.5px] text-t26">
        {props.count.toLocaleString()} buffered
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
