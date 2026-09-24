import { describe, expect, it, vi, beforeEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { PaginatedResult } from '@/types'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return { ...actual, getRecordsPaginated: vi.fn() }
})

import * as api from '@/services/api'
import { useRecordsLoader } from './useRecordsLoader'

const pageOf = (page: number, size: number, total: number): PaginatedResult => ({
  items: Array.from({ length: Math.max(0, Math.min(size, total - (page - 1) * size)) }, (_, i) => ({ id: (page - 1) * size + i + 1 })),
  total,
  page,
  page_size: size,
  total_pages: Math.ceil(total / size),
  has_more: page * size < total,
})

describe('useRecordsLoader', () => {
  const mocked = vi.mocked(api.getRecordsPaginated)

  beforeEach(() => {
    mocked.mockReset()
  })

  it('appends the next page on loadMore instead of replacing the rows', async () => {
    mocked.mockImplementation(async (_name, params) => pageOf(params.page ?? 1, params.page_size ?? 2, 5))

    const { result } = renderHook(() => useRecordsLoader({
      modelName: 'Note', pageSize: 2, search: '', filters: {}, orderBy: '',
    }))

    await waitFor(() => expect(result.current.rows).toHaveLength(2))
    expect(result.current.hasMore).toBe(true)

    act(() => result.current.loadMore())
    await waitFor(() => expect(result.current.rows).toHaveLength(4))
    expect(result.current.rows.map((r) => r.id)).toEqual([1, 2, 3, 4])
    expect(result.current.page).toBe(2)

    act(() => result.current.loadMore())
    await waitFor(() => expect(result.current.rows).toHaveLength(5))
    expect(result.current.hasMore).toBe(false)
    expect(mocked).toHaveBeenLastCalledWith('Note', expect.objectContaining({ page: 3, page_size: 2 }), expect.any(AbortSignal))
  })

  it('sends order_by and resets to page one when the sort changes', async () => {
    mocked.mockImplementation(async (_name, params) => pageOf(params.page ?? 1, params.page_size ?? 2, 5))

    const { result, rerender } = renderHook((props: { orderBy: string }) => useRecordsLoader({
      modelName: 'Note', pageSize: 2, search: '', filters: {}, orderBy: props.orderBy,
    }), { initialProps: { orderBy: '' } })

    await waitFor(() => expect(result.current.rows).toHaveLength(2))
    act(() => result.current.loadMore())
    await waitFor(() => expect(result.current.rows).toHaveLength(4))

    rerender({ orderBy: 'title desc' })
    await waitFor(() => expect(mocked).toHaveBeenLastCalledWith(
      'Note', expect.objectContaining({ page: 1, order_by: 'title desc' }), expect.any(AbortSignal),
    ))
    await waitFor(() => expect(result.current.rows).toHaveLength(2))
    expect(result.current.page).toBe(1)
  })

  it('drops a stale response that resolves after a newer request', async () => {
    let resolveFirst: (r: PaginatedResult) => void = () => {}
    mocked
      .mockImplementationOnce(() => new Promise<PaginatedResult>((resolve) => { resolveFirst = resolve }))
      .mockImplementationOnce(async () => ({ ...pageOf(1, 2, 2), items: [{ id: 'fresh' }] }))

    const { result, rerender } = renderHook((props: { search: string }) => useRecordsLoader({
      modelName: 'Note', pageSize: 2, search: props.search, filters: {}, orderBy: '',
    }), { initialProps: { search: '' } })

    rerender({ search: 'x' })
    await waitFor(() => expect(result.current.rows.map((r) => r.id)).toEqual(['fresh']))

    act(() => resolveFirst({ ...pageOf(1, 2, 2), items: [{ id: 'stale' }] }))
    await new Promise((r) => setTimeout(r, 10))
    expect(result.current.rows.map((r) => r.id)).toEqual(['fresh'])
  })
})
