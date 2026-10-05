import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import pako from 'pako'
import type { Plugin, ResolvedConfig } from 'vite'

/**
 * Writes, next to every text file of the built dist, the two encodings a
 * browser asks for — `<file>.gz` and `<file>.br` — so the Go servers that
 * embed the dist can answer `Accept-Encoding` with bytes compressed once, at
 * build time, instead of compressing on every request or not at all.
 *
 * The dist is committed and CI rebuilds it and compares byte for byte, so
 * both compressors are pinned code that gives the same bytes on any machine:
 * pako (zlib's deflate, ported to JavaScript; level 9, no name, mtime 0) and
 * brotli-wasm (Brotli compiled to WebAssembly; quality 11). Node's own zlib
 * would not do: its output follows the zlib and Brotli it was built with,
 * which differ between Node releases and between a laptop and the runner.
 *
 * The rule the Go side checks (ui/embed_test.go): a file whose extension is
 * in COMPRESSIBLE and whose size is at least MIN_BYTES has both variants;
 * a smaller one has neither and travels as it is. Each variant is decoded
 * back here before it is written, and the build fails if it does not give
 * the file's bytes.
 */

/** Text formats worth compressing. Fonts (woff2) and images already are. */
export const COMPRESSIBLE = /\.(?:js|mjs|css|html|svg|json|txt|xml|webmanifest)$/

/** Below this size a compressed copy saves less than its own headers cost. */
export const MIN_BYTES = 1024

const require = createRequire(import.meta.url)
// The Node build of brotli-wasm loads its WebAssembly synchronously; the ESM
// entry the `import` condition resolves to is the browser one.
const brotli = require('brotli-wasm') as {
  compress(buf: Uint8Array, options?: { quality?: number }): Uint8Array
  decompress(buf: Uint8Array): Uint8Array
}

/** wantsVariants reports whether a file of this name and size gets a .gz and a .br. */
export function wantsVariants(name: string, size: number): boolean {
  return COMPRESSIBLE.test(name) && size >= MIN_BYTES
}

function same(a: Uint8Array, b: Uint8Array): boolean {
  return a.length === b.length && a.every((byte, i) => byte === b[i])
}

/** encode returns the gzip and Brotli encodings of raw, each checked by decoding it. */
export function encode(raw: Uint8Array, name = 'input'): { gzip: Uint8Array; brotli: Uint8Array } {
  const gzip = pako.gzip(raw, { level: 9 })
  const br = brotli.compress(raw, { quality: 11 })
  if (!same(pako.ungzip(gzip), raw)) throw new Error(`precompress: the gzip encoding of ${name} does not decode to it`)
  if (!same(brotli.decompress(br), raw)) throw new Error(`precompress: the Brotli encoding of ${name} does not decode to it`)
  return { gzip, brotli: br }
}

/** precompressDir writes the variants for every file under dir that wants them, and returns their names. */
export function precompressDir(dir: string): string[] {
  const written: string[] = []
  const walk = (current: string) => {
    for (const entry of readdirSync(current, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const full = path.join(current, entry.name)
      if (entry.isDirectory()) {
        walk(full)
        continue
      }
      if (entry.name.endsWith('.gz') || entry.name.endsWith('.br')) continue
      const raw = readFileSync(full)
      if (!wantsVariants(entry.name, raw.length)) continue
      const { gzip, brotli: br } = encode(raw, path.relative(dir, full))
      writeFileSync(`${full}.gz`, gzip)
      writeFileSync(`${full}.br`, br)
      written.push(path.relative(dir, full))
    }
  }
  walk(dir)
  return written
}

/** precompress is the Vite plugin: once the bundle is on disk, every entry's
 * outDir gets its variants. The outDir is emptied by the build first, so no
 * variant of a file that is gone survives. */
export function precompress(): Plugin {
  let outDir = ''
  return {
    name: 'orbit-precompress',
    apply: 'build',
    enforce: 'post',
    configResolved(config: ResolvedConfig) {
      outDir = path.resolve(config.root, config.build.outDir)
    },
    closeBundle() {
      const written = precompressDir(outDir)
      this.info?.(`precompressed ${written.length} files in ${path.basename(outDir)} (.gz and .br)`)
    },
  }
}
