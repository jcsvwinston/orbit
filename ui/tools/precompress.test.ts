import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { gunzipSync, brotliDecompressSync } from 'node:zlib'
import { afterEach, describe, expect, it } from 'vitest'
import { MIN_BYTES, encode, precompressDir, wantsVariants } from './precompress'

const dirs: string[] = []
function scratch(): string {
  const dir = mkdtempSync(path.join(tmpdir(), 'orbit-precompress-'))
  dirs.push(dir)
  return dir
}
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true })
})

const text = (n: number) => 'const value = "orbit";\n'.repeat(Math.ceil(n / 23)).slice(0, n)

describe('precompress', () => {
  it('encodes to bytes any decoder gives back', () => {
    const raw = Buffer.from(text(8192))
    const { gzip, brotli } = encode(raw)
    // Decoded by Node's own zlib, not by the libraries that encoded them.
    expect(gunzipSync(gzip).equals(raw)).toBe(true)
    expect(brotliDecompressSync(brotli).equals(raw)).toBe(true)
    expect(gzip.length).toBeLessThan(raw.length)
    expect(brotli.length).toBeLessThan(raw.length)
  })

  it('gives the same bytes every time: no name and no time in the gzip header', () => {
    const raw = Buffer.from(text(4096))
    const first = encode(raw)
    const second = encode(Buffer.from(raw))
    expect(Buffer.from(first.gzip).equals(Buffer.from(second.gzip))).toBe(true)
    expect(Buffer.from(first.brotli).equals(Buffer.from(second.brotli))).toBe(true)
    // MTIME (bytes 4..7) is zero and FLG (byte 3) carries no FNAME.
    expect(Array.from(first.gzip.slice(3, 8))).toEqual([0, 0, 0, 0, 0])
  })

  it('compresses text files of at least MIN_BYTES and leaves the rest', () => {
    expect(wantsVariants('assets/index-abc.js', MIN_BYTES)).toBe(true)
    expect(wantsVariants('assets/index-abc.css', MIN_BYTES - 1)).toBe(false)
    expect(wantsVariants('favicon.svg', 4096)).toBe(true)
    expect(wantsVariants('assets/font.woff2', 65536)).toBe(false)
    expect(wantsVariants('assets/logo.png', 65536)).toBe(false)
  })

  it('writes both variants beside each file that wants them, and nothing beside the others', () => {
    const dir = scratch()
    mkdirSync(path.join(dir, 'assets'))
    writeFileSync(path.join(dir, 'assets', 'big.js'), text(MIN_BYTES * 4))
    writeFileSync(path.join(dir, 'assets', 'tiny.js'), 'export {}\n')
    writeFileSync(path.join(dir, 'assets', 'font.woff2'), Buffer.alloc(MIN_BYTES * 4, 7))
    writeFileSync(path.join(dir, 'index.html'), `<!doctype html>${text(MIN_BYTES)}`)

    const written = precompressDir(dir)
    expect(written.sort()).toEqual([path.join('assets', 'big.js'), 'index.html'])
    expect(readdirSync(path.join(dir, 'assets')).sort()).toEqual(['big.js', 'big.js.br', 'big.js.gz', 'font.woff2', 'tiny.js'])
    expect(gunzipSync(readFileSync(path.join(dir, 'assets', 'big.js.gz'))).toString()).toBe(text(MIN_BYTES * 4))
    expect(existsSync(path.join(dir, 'index.html.br'))).toBe(true)

    // A second pass over the same tree does not compress the variants.
    expect(precompressDir(dir).sort()).toEqual([path.join('assets', 'big.js'), 'index.html'])
    expect(readdirSync(path.join(dir, 'assets')).filter((n) => n.endsWith('.gz.gz') || n.endsWith('.br.gz'))).toEqual([])
  })
})
