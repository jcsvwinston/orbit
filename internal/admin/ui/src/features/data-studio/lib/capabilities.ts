import type { ModelSchema, SchemaField } from '@/types'

// What a model screen may offer this operator.
//
// The backend adds capability hints to the schema a screen loads
// (internal/admin/permissions_hints.go), so the panel can disable what would
// only ever come back as a 403. They are a rendering aid: every one of them is
// enforced again on the request that follows, and a panel that answers none of
// them — an older backend, or a posture with no authorization at all — leaves
// them undefined, which reads here as "allowed". The grid then draws exactly
// what it drew before hints existed.
export interface ScreenCapabilities {
  canCreate: boolean
  canUpdate: boolean
  canDelete: boolean
  // True when at least one verb is granted over the operator's OWN rows only,
  // which is worth saying on screen: the list is not the whole table.
  rowScoped: boolean
}

export function screenCapabilities(schema: ModelSchema): ScreenCapabilities {
  const readOnly = schema.read_only === true
  return {
    canCreate: !readOnly && schema.can_create !== false,
    canUpdate: !readOnly && schema.can_update !== false,
    canDelete: !readOnly && schema.can_delete !== false,
    rowScoped: (schema.row_scope?.length ?? 0) > 0,
  }
}

// isFieldEditable answers the FIELD question only: whether a policy keeps this
// operator out of this column. Whether they may write the model at all is
// canUpdate, and the two are deliberately separate — folding them together
// drew an empty create form for an operator who may create but not update.
export function isFieldEditable(field: SchemaField): boolean {
  return field.can_edit !== false
}
