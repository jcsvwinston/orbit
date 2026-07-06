// Terminal-style sparkline (design handoff): SVG polyline 1.6px stroke plus a
// 12%-opacity area fill underneath. Samples are plotted oldest→newest.
export function Sparkline(props: {
  data: ReadonlyArray<number>
  width: number
  height: number
  color: string
  strokeWidth?: number
}) {
  const { data, width: w, height: h } = props
  if (data.length < 2) {
    return <svg width={w} height={h} aria-hidden />
  }
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const pad = 2
  const step = (w - pad * 2) / (data.length - 1)
  const pts = data.map((v, i) => {
    const x = pad + i * step
    const y = pad + (h - pad * 2) * (1 - (v - min) / span)
    return [x, y] as const
  })
  const line = pts.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = `${pad},${h - pad} ${line} ${(pad + (data.length - 1) * step).toFixed(1)},${h - pad}`
  return (
    <svg width={w} height={h} aria-hidden className="shrink-0">
      <polygon points={area} fill={props.color} opacity={0.12} />
      <polyline
        points={line}
        fill="none"
        stroke={props.color}
        strokeWidth={props.strokeWidth ?? 1.6}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  )
}
