import { describe, expect, it } from 'vitest'
import { translate } from './messagesStore'

// The fallback chain is what keeps a half-translated panel readable: the
// catalogue, then the English written at the call site. A key that reaches
// the screen as `nav.audit` would be worse than the English it replaced.
describe('translate', () => {
  it('uses the catalogue when it has the phrase', () => {
    expect(translate({ 'nav.audit': 'Auditoría' }, 'nav.audit', 'Audit Log')).toBe('Auditoría')
  })

  it('falls back to the call site for a key nobody translated', () => {
    expect(translate({}, 'nav.audit', 'Audit Log')).toBe('Audit Log')
  })

  it('treats an empty translation as no translation', () => {
    expect(translate({ 'nav.audit': '   ' }, 'nav.audit', 'Audit Log')).toBe('Audit Log')
  })
})
