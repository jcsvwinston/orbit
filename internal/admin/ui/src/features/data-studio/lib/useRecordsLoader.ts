import { useCallback, useEffect, useRef, useState } from 'react'
import * as api from '@/services/api'
import type { Record as AppRecord } from '@/types'

export interface RecordsQueryState {
  modelName: string
  dbAlias?: string
  pageSize: number
  search: string
  filters: { [column: string]: string }
  orderBy: string
}

export interface RecordsLoader {
  rows: AppRecord[]
  total: number
  isEstimated: boolean
  hasMore: boolean
  page: number
  loading: boolean
  loadingMore: boolean
  error: unknown
  reload: () => void
  loadMore: () => void
}

function cleanFilters(filters: { [column: string]: string }): { [column: string]: string } | undefined {
  const out: { [column: string]: string } = {}
  for (const [k, v] of Object.entries(filters)) {
    if (v.trim()) out[k] = v
  }
  return Object.keys(out).length > 0 ? out : undefined
}

// useRecordsLoader is the single loader for the data-studio grid. One
// in-flight request at a time: a new query aborts the previous fetch, and a
// monotonic request id drops any response that still lands out of order.
// "Load more" appends the next page to `rows`; every other change resets.
export function useRecordsLoader(query: RecordsQueryState): RecordsLoader {
  const [rows, setRows] = useState<AppRecord[]>([])
  const [total, setTotal] = useState(0)
  const [isEstimated, setIsEstimated] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const controllerRef = useRef<AbortController | null>(null)
  const requestIdRef = useRef(0)
  const pageRef = useRef(1)
  const queryRef = useRef(query)
  queryRef.current = query

  const load = useCallback(async (mode: 'reset' | 'more') => {
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    const requestId = ++requestIdRef.current
    const q = queryRef.current
    const targetPage = mode === 'more' ? pageRef.current + 1 : 1

    if (mode === 'more') setLoadingMore(true)
    else setLoading(true)
    setError(null)

    try {
      const res = await api.getRecordsPaginated(q.modelName, {
        page: targetPage,
        page_size: q.pageSize,
        search: q.search || undefined,
        order_by: q.orderBy || undefined,
        db_alias: q.dbAlias,
        filters: cleanFilters(q.filters),
      }, controller.signal)
      if (requestId !== requestIdRef.current) return
      const items = res.items ?? []
      setRows((prev) => (mode === 'more' ? [...prev, ...items] : items))
      setTotal(res.total ?? 0)
      setIsEstimated(res.is_estimated ?? false)
      setHasMore(res.has_more ?? false)
      pageRef.current = targetPage
      setPage(targetPage)
    } catch (err) {
      if (api.isAbortError(err) || requestId !== requestIdRef.current) return
      setError(err)
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [])

  const filtersKey = JSON.stringify(query.filters)
  useEffect(() => {
    load('reset')
  }, [load, query.modelName, query.dbAlias, query.pageSize, query.search, query.orderBy, filtersKey])

  useEffect(() => () => controllerRef.current?.abort(), [])

  return {
    rows,
    total,
    isEstimated,
    hasMore,
    page,
    loading,
    loadingMore,
    error,
    reload: () => { load('reset') },
    loadMore: () => { load('more') },
  }
}
