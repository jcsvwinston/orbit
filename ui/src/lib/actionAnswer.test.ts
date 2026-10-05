import { describe, expect, it } from 'vitest'
import { filenameFromDisposition, isAttachment, redirectDestination } from './actionAnswer'

const roots = ['data-studio', 'audit']

describe('redirectDestination', () => {
  it('follows a screen of the SPA through the router', () => {
    expect(redirectDestination('/data-studio?model=Note&record=9', roots)).toEqual({ router: true, to: '/data-studio?model=Note&record=9' })
    expect(redirectDestination('/', roots)).toEqual({ router: true, to: '/' })
    expect(redirectDestination('/audit#top', roots)).toEqual({ router: true, to: '/audit#top' })
  })

  // A page the server serves inside the panel — an application's own
  // screen — is a document, under the panel's prefix.
  it('loads any other page of the panel as a document', () => {
    expect(redirectDestination('/x/reports/batch/7', roots)).toEqual({ router: false, href: '/admin/x/reports/batch/7' })
  })

  // The server refuses these first; the SPA does not follow one if a server
  // ever let it through.
  it('follows nothing that leaves the panel', () => {
    for (const bad of ['https://evil.example', '//evil.example', '///evil.example', '/\\evil.example', 'evil.example', 'javascript:alert(1)', '/a/../../b', '/x\n/y', '']) {
      expect(redirectDestination(bad, roots)).toBeNull()
    }
  })
})

describe('Content-Disposition', () => {
  it('tells a file from an answer', () => {
    expect(isAttachment('attachment; filename="a.txt"')).toBe(true)
    expect(isAttachment('inline')).toBe(false)
    expect(isAttachment(null)).toBe(false)
  })

  it('reads the exact name, then the fallback, and keeps only the last element', () => {
    expect(filenameFromDisposition(`attachment; filename="R_sum_.txt"; filename*=UTF-8''R%C3%A9sum%C3%A9.txt`)).toBe('Résumé.txt')
    expect(filenameFromDisposition('attachment; filename="note \\"7\\".txt"')).toBe('note "7".txt')
    expect(filenameFromDisposition('attachment; filename=plain.csv')).toBe('plain.csv')
    expect(filenameFromDisposition('attachment; filename="../../etc/passwd"')).toBe('passwd')
    expect(filenameFromDisposition('attachment; filename=".."')).toBeNull()
    expect(filenameFromDisposition('attachment')).toBeNull()
  })
})
