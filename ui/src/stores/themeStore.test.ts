import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CHOICE_KEY, LAST_THEME_KEY, applyInitialTheme, documentTheme, useTheme } from './themeStore'

function reset() {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
  document.documentElement.removeAttribute('data-theme-from')
}

describe('applyInitialTheme', () => {
  beforeEach(reset)
  afterEach(() => {
    reset()
    vi.unstubAllGlobals()
  })

  // Without a configured theme the panel decides as it always has.
  it('opens in the last theme the panel showed', () => {
    localStorage.setItem(LAST_THEME_KEY, 'dark')
    applyInitialTheme()
    expect(documentTheme()).toBe('dark')
  })

  it("follows the browser's preference on a first visit, and keeps it", () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('dark') }))
    applyInitialTheme()
    expect(documentTheme()).toBe('dark')
    expect(localStorage.getItem(LAST_THEME_KEY)).toBe('dark')
  })

  // A configured theme was applied by the document ahead of the bundle; the
  // bundle deciding again is the switch after the first frame.
  it('leaves a theme the document already decided alone', () => {
    document.documentElement.classList.add('dark')
    document.documentElement.setAttribute('data-theme-from', 'configuration')
    localStorage.setItem(LAST_THEME_KEY, 'light')
    applyInitialTheme()
    expect(documentTheme()).toBe('dark')
    expect(localStorage.getItem(LAST_THEME_KEY)).toBe('light')
  })
})

describe('useTheme', () => {
  beforeEach(reset)
  afterEach(reset)

  it('starts from the theme the document is painted in', () => {
    document.documentElement.classList.add('dark')
    useTheme.getState().initTheme()
    expect(useTheme.getState().theme).toBe('dark')
  })

  // The toggle is the only thing that records a choice: that is what lets a
  // configured theme apply to an operator who never chose, and never over
  // one who did.
  it('records the toggle as the operator\'s choice', () => {
    useTheme.getState().initTheme()
    useTheme.getState().toggleTheme()
    expect(documentTheme()).toBe('dark')
    expect(localStorage.getItem(CHOICE_KEY)).toBe('dark')
    expect(localStorage.getItem(LAST_THEME_KEY)).toBe('dark')
    useTheme.getState().toggleTheme()
    expect(localStorage.getItem(CHOICE_KEY)).toBe('light')
  })
})
