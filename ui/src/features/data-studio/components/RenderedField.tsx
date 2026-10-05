import { useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import {
  renderFieldValue,
  renderersRevision,
  subscribeRenderers,
  type FieldRenderWhere,
} from '@/lib/clientExtensions'

interface Props {
  /** The application's renderer the schema names for this field. */
  renderer: string
  value: unknown
  model: string
  field: string
  column: string
  record: Readonly<Record<string, unknown>> | null
  where: FieldRenderWhere
  /** What the panel draws on its own: shown when the renderer is missing or
   * fails. */
  fallback: ReactNode
}

type Shown = { kind: 'drawn' } | { kind: 'missing' } | { kind: 'failed'; error: string }

// A renderer the schema names and no script registered is the application's
// mistake, not the operator's: the value is drawn the panel's way and the
// console says why, once per name.
const warnedMissing = new Set<string>()

/**
 * RenderedField draws one value with the application's renderer (EXT-07).
 *
 * The renderer runs in a layout effect, inside a try, and what it returns is
 * put into an element React does not manage: a renderer that throws, or
 * returns something that is not a node or a string, costs that one value —
 * the panel's own drawing and a line that says the renderer failed, in the
 * same place — never the grid or the record view around it.
 */
export default function RenderedField({ renderer, value, model, field, column, record, where, fallback }: Props) {
  const revision = useSyncExternalStore(subscribeRenderers, renderersRevision)
  const host = useRef<HTMLSpanElement>(null)
  const [shown, setShown] = useState<Shown>({ kind: 'missing' })

  useLayoutEffect(() => {
    const el = host.current
    if (!el) return
    const outcome = renderFieldValue(renderer, value, { model, field, column, record, where })
    if (outcome.kind === 'drawn') {
      el.replaceChildren(outcome.node)
      setShown((prev) => (prev.kind === 'drawn' ? prev : { kind: 'drawn' }))
      return
    }
    el.replaceChildren()
    if (outcome.kind === 'missing') {
      if (!warnedMissing.has(renderer)) {
        warnedMissing.add(renderer)
        console.warn(`orbit: the schema draws ${model}.${field} with the field renderer "${renderer}", and no script registered it`)
      }
      setShown((prev) => (prev.kind === 'missing' ? prev : { kind: 'missing' }))
      return
    }
    console.error(`orbit: the field renderer "${renderer}" failed on ${model}.${field}: ${outcome.error}`)
    setShown((prev) => (prev.kind === 'failed' && prev.error === outcome.error ? prev : outcome))
  }, [renderer, value, model, field, column, record, where, revision])

  return (
    <span className="inline-flex max-w-full items-center gap-1.5" data-field-renderer={renderer} data-renderer-state={shown.kind}>
      {/* What the renderer drew; empty when it drew nothing. */}
      <span ref={host} className="contents" />
      {shown.kind !== 'drawn' && <span className="truncate">{fallback}</span>}
      {shown.kind === 'failed' && (
        <span className="truncate text-xs text-destructive-text" title={shown.error}>
          {`${renderer} failed: ${shown.error}`}
        </span>
      )}
    </span>
  )
}
