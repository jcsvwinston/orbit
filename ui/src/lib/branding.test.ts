import { afterEach, describe, expect, it } from 'vitest'
import { applyBranding, foregroundFor, hexToHsl } from './branding'

describe('hexToHsl', () => {
  it('converts both hex forms to the property triple', () => {
    expect(hexToHsl('#000000')).toBe('0 0% 0%')
    expect(hexToHsl('#fff')).toBe('0 0% 100%')
    expect(hexToHsl('#0b5fff')).toBe('219.3 100% 52.2%')
  })

  // The backend validates the value at startup, so anything else here means
  // the page was served by something other than the panel: keep its colour
  // rather than write a broken property.
  it('refuses what is not a hex colour', () => {
    expect(hexToHsl('red')).toBeNull()
    expect(hexToHsl('#12345')).toBeNull()
    expect(hexToHsl('')).toBeNull()
  })
})

describe('foregroundFor', () => {
  // A brand colour is chosen to look like a brand, not to contrast with
  // white: white text on a pale yellow button is a contrast failure the
  // panel would have introduced on the application's behalf.
  it('darkens the text on a light brand colour', () => {
    expect(foregroundFor('54 100% 62%')).toBe('222.2 47.4% 11.2%')
  })

  it('keeps it white on a dark one', () => {
    expect(foregroundFor('220.5 100% 52.5%')).toBe('0 0% 100%')
  })

  // Chosen by lightness, these got white text: a saturated yellow, cyan or
  // green sits at 50% and reads at 1.07, 1.25 and 1.37 to 1 under white.
  it('picks the ink that reads, not the one the lightness suggests', () => {
    expect(foregroundFor('60 100% 50%')).toBe('222.2 47.4% 11.2%')
    expect(foregroundFor('180 100% 50%')).toBe('222.2 47.4% 11.2%')
    expect(foregroundFor('120 100% 50%')).toBe('222.2 47.4% 11.2%')
  })
})

describe('applyBranding', () => {
  afterEach(() => {
    document.documentElement.removeAttribute('style')
    document.getElementById('orbit-palette')?.remove()
  })

  it('paints the accent on the document', () => {
    applyBranding({ logo: '', favicon: '', primaryColor: '#0b5fff' })
    expect(document.documentElement.style.getPropertyValue('--primary')).toBe('219.3 100% 52.2%')
  })

  // A palette per theme arrives as a stylesheet the backend wrote, which
  // already carries the accent of each theme: an inline property would paint
  // one accent over both again.
  it('leaves the accent to a palette per theme', () => {
    const style = document.createElement('style')
    style.id = 'orbit-palette'
    document.head.appendChild(style)
    applyBranding({ logo: '', favicon: '', primaryColor: '#0b5fff' })
    expect(document.documentElement.style.getPropertyValue('--primary')).toBe('')
  })
})
