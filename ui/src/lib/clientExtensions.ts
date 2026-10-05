/**
 * The application's own code in the panel (EXT-06), and the field renderers
 * it registers (EXT-07).
 *
 * The server names the application's scripts on the document after this
 * bundle, deferred and with the digest of their bytes. The bundle is a module,
 * so the browser runs it once the document is parsed, and a deferred script
 * after it runs after it: by the time an application's script runs,
 * `window.orbit` is here.
 *
 * `window.orbit` is the whole of the contract, and it is small on purpose:
 *
 *   window.orbit.version                       // 1
 *   window.orbit.registerFieldRenderer(name, render)
 *
 * A renderer draws one field's value — in the list and on the record view —
 * and returns a DOM node or a string (a string is drawn as text, never parsed
 * as markup). It is called with the value and a context: the model, the
 * field, its column, the record (a frozen copy) and where it is drawn
 * ("list" or "record"). A renderer that throws, or returns anything else,
 * costs that one value: the panel draws it its own way and says, in the same
 * place, that the renderer failed. A renderer registered after the panel
 * drew is applied when it is registered.
 *
 * The panel only asks for a renderer the schema names for a field, and the
 * schema only names one the application declared in Go
 * (ClientCode.FieldRenderers): registering any other name does nothing.
 */

/** The version of `window.orbit`. A later contract that changes what a
 * renderer receives or returns is a new version; a script that needs one
 * checks `window.orbit.version` first. */
export const CLIENT_API_VERSION = 1

/** Where a value is being drawn. */
export type FieldRenderWhere = 'list' | 'record'

/** What a renderer is told about the value it draws. */
export interface FieldRenderContext {
  model: string
  field: string
  column: string
  record: Readonly<Record<string, unknown>> | null
  where: FieldRenderWhere
}

/** A renderer draws one value: a DOM node, or a string drawn as text. */
export type FieldRenderer = (value: unknown, context: FieldRenderContext) => Node | string

/** `window.orbit`. */
export interface OrbitClientAPI {
  readonly version: number
  registerFieldRenderer(name: string, render: FieldRenderer): void
}

declare global {
  interface Window {
    orbit?: OrbitClientAPI
  }
}

/** A renderer name: what a field_widgets value can carry. The server refuses
 * any other at startup; this says so to the script that tries one. */
const RENDERER_NAME = /^[a-z][a-z0-9-]{0,63}$/

const renderers = new Map<string, FieldRenderer>()
const listeners = new Set<() => void>()
let revision = 0

/** registerFieldRenderer is `window.orbit.registerFieldRenderer`. A second
 * registration under one name replaces the first. A name or a renderer that
 * cannot be one throws, so the script that made the mistake hears of it. */
export function registerFieldRenderer(name: string, render: FieldRenderer): void {
  if (typeof name !== 'string' || !RENDERER_NAME.test(name)) {
    throw new TypeError(
      `orbit.registerFieldRenderer: ${JSON.stringify(name)} is not a renderer name (lowercase letters, digits or dashes, starting with a letter)`,
    )
  }
  if (typeof render !== 'function') {
    throw new TypeError(`orbit.registerFieldRenderer(${JSON.stringify(name)}): the renderer is not a function`)
  }
  renderers.set(name, render)
  revision++
  listeners.forEach((listener) => listener())
}

/** subscribeRenderers is told every time a renderer is registered, so a
 * value drawn before its renderer arrived is drawn again. */
export function subscribeRenderers(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** renderersRevision changes with every registration. */
export function renderersRevision(): number {
  return revision
}

/** What drawing one value came to. */
export type RenderOutcome =
  | { kind: 'drawn'; node: Node }
  | { kind: 'missing' }
  | { kind: 'failed'; error: string }

function describe(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'an array'
  if (typeof value === 'object') return 'an object'
  return typeof value === 'undefined' ? 'nothing' : `a ${typeof value}`
}

/** renderFieldValue asks the named renderer to draw one value. It never
 * throws: what the renderer did wrong is the outcome. */
export function renderFieldValue(name: string, value: unknown, context: FieldRenderContext): RenderOutcome {
  const render = renderers.get(name)
  if (!render) return { kind: 'missing' }
  const record = context.record ? Object.freeze({ ...context.record }) : null
  try {
    const out = render(value, { ...context, record })
    if (typeof out === 'string') return { kind: 'drawn', node: document.createTextNode(out) }
    if (out instanceof Node) return { kind: 'drawn', node: out }
    return { kind: 'failed', error: `returned ${describe(out)}, not a DOM node or a string` }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    return { kind: 'failed', error: message || 'threw' }
  }
}

/** installClientAPI puts `window.orbit` in place: read-only, so a script
 * cannot replace it for the scripts after it by accident. Called once, from
 * the bundle's entry, before anything else runs. */
export function installClientAPI(target: Window = window): void {
  if (target.orbit) return
  const api: OrbitClientAPI = Object.freeze({ version: CLIENT_API_VERSION, registerFieldRenderer })
  Object.defineProperty(target, 'orbit', { value: api, enumerable: true, configurable: false, writable: false })
}

/** forgetFieldRenderers empties the registry, for tests. */
export function forgetFieldRenderers(): void {
  renderers.clear()
  revision++
  listeners.forEach((listener) => listener())
}
