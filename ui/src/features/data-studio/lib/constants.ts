// The list endpoint rejects page_size above this (internal/admin/handlers.go:
// "page_size must be <= 200"), so the batch selector must not offer more.
export const MAX_PAGE_SIZE = 200
export const BATCH_SIZE_OPTIONS = [25, 50, 100, 200] as const
export const DEFAULT_PAGE_SIZE = 50
export const FILTER_DEBOUNCE_MS = 300
