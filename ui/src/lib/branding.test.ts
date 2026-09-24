import { describe, expect, it } from 'vitest'
import { foregroundFor, hexToHsl } from './branding'

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
})
