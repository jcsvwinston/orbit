// Semantic stream/status colors (design handoff), resolved per theme via the
// CSS token variables. Lives in lib/ so component files only export components
// (react-refresh rule).
export const SEMANTIC = {
  blue: 'var(--t48)',
  green: 'var(--t49)',
  amber: 'var(--t50)',
  red: 'var(--t51)',
  violet: 'var(--t52)',
  accent: 'var(--accent)',
} as const

export type SemanticColor = keyof typeof SEMANTIC

export function methodColor(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
      return SEMANTIC.blue
    case 'POST':
      return SEMANTIC.green
    case 'PUT':
    case 'PATCH':
      return SEMANTIC.amber
    case 'DELETE':
      return SEMANTIC.red
    default:
      return SEMANTIC.violet
  }
}

export function sqlKindColor(op: string): string {
  switch (op.toUpperCase()) {
    case 'SELECT':
      return SEMANTIC.blue
    case 'INSERT':
      return SEMANTIC.green
    case 'UPDATE':
      return SEMANTIC.amber
    case 'DELETE':
      return SEMANTIC.red
    default:
      return SEMANTIC.violet
  }
}

export function statusColor(status: number): string {
  if (status >= 500) return SEMANTIC.red
  if (status >= 400) return SEMANTIC.amber
  if (status >= 300) return SEMANTIC.blue
  return SEMANTIC.green
}
