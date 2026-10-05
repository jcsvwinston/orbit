import { afterEach, describe, expect, it } from 'vitest'
import {
  CLIENT_API_VERSION,
  forgetFieldRenderers,
  installClientAPI,
  registerFieldRenderer,
  renderFieldValue,
  renderersRevision,
  subscribeRenderers,
  type FieldRenderContext,
} from './clientExtensions'

const context: FieldRenderContext = {
  model: 'Note', field: 'Status', column: 'status', record: { id: 1, status: 'draft' }, where: 'list',
}

afterEach(() => forgetFieldRenderers())

describe('window.orbit', () => {
  it('is installed once, read-only, with its version', () => {
    const target = {} as Window
    installClientAPI(target)
    const api = target.orbit
    expect(api?.version).toBe(CLIENT_API_VERSION)
    expect(CLIENT_API_VERSION).toBe(1)
    expect(Object.isFrozen(api)).toBe(true)
    installClientAPI(target)
    expect(target.orbit).toBe(api)
    expect(() => { (target as { orbit?: unknown }).orbit = {} }).toThrow()
  })

  it('registers through the installed API', () => {
    const target = {} as Window
    installClientAPI(target)
    target.orbit?.registerFieldRenderer('badge', (v) => `[${String(v)}]`)
    const out = renderFieldValue('badge', 'draft', context)
    expect(out.kind).toBe('drawn')
    expect(out.kind === 'drawn' && out.node.textContent).toBe('[draft]')
  })
})

describe('registerFieldRenderer', () => {
  it('refuses a name a field_widgets value cannot carry, and a renderer that is not a function', () => {
    expect(() => registerFieldRenderer('Badge', () => '')).toThrow(/is not a renderer name/)
    expect(() => registerFieldRenderer('', () => '')).toThrow(/is not a renderer name/)
    expect(() => registerFieldRenderer('badge', 'nope' as unknown as () => string)).toThrow(/not a function/)
  })

  it('tells the subscribers, so a value drawn before its renderer arrived is drawn again', () => {
    let told = 0
    const stop = subscribeRenderers(() => { told++ })
    const before = renderersRevision()
    registerFieldRenderer('badge', () => '')
    expect(told).toBe(1)
    expect(renderersRevision()).not.toBe(before)
    stop()
    registerFieldRenderer('money', () => '')
    expect(told).toBe(1)
  })
})

describe('renderFieldValue', () => {
  it('draws a node as given and a string as text, never as markup', () => {
    registerFieldRenderer('badge', (value) => {
      const el = document.createElement('b')
      el.textContent = String(value)
      return el
    })
    registerFieldRenderer('text', () => '<img src=x onerror=alert(1)>')
    const node = renderFieldValue('badge', 'draft', context)
    expect(node.kind === 'drawn' && (node.node as HTMLElement).outerHTML).toBe('<b>draft</b>')
    const text = renderFieldValue('text', 'x', context)
    expect(text.kind === 'drawn' && text.node.nodeType).toBe(Node.TEXT_NODE)
  })

  it('hands the renderer the value, the context and a record it cannot change', () => {
    let seen: FieldRenderContext | null = null
    registerFieldRenderer('spy', (_value, ctx) => {
      seen = ctx
      return ''
    })
    renderFieldValue('spy', 'draft', context)
    expect(seen).toMatchObject({ model: 'Note', field: 'Status', column: 'status', where: 'list' })
    expect(Object.isFrozen((seen as unknown as FieldRenderContext).record)).toBe(true)
    expect(Object.isFrozen(context.record)).toBe(false)
  })

  it('turns what the renderer did wrong into the outcome, and never throws', () => {
    registerFieldRenderer('throws', () => { throw new Error('no badge for "archived"') })
    registerFieldRenderer('object', () => ({}) as unknown as Node)
    registerFieldRenderer('nothing', () => undefined as unknown as Node)
    expect(renderFieldValue('throws', 'archived', context)).toEqual({ kind: 'failed', error: 'no badge for "archived"' })
    expect(renderFieldValue('object', 1, context)).toEqual({ kind: 'failed', error: 'returned an object, not a DOM node or a string' })
    expect(renderFieldValue('nothing', 1, context)).toEqual({ kind: 'failed', error: 'returned nothing, not a DOM node or a string' })
    expect(renderFieldValue('unregistered', 1, context)).toEqual({ kind: 'missing' })
  })
})
