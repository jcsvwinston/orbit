import type { CapabilityHints, ModelSchema, SchemaField } from '@/types'

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
  // canBulkDelete is the delete of a selection. The server authorizes it as
  // a verb of its own (bulk_delete), not as delete: an operator who may
  // delete one record at a time and not a batch is refused the batch, so the
  // grid must not offer it — and one who holds bulk_delete alone may.
  canBulkDelete: boolean
  // canRetrieve is opening one record. An operator who may read a record and
  // not update it is offered its view read-only; without it the only way to
  // a record's fields is the columns the list shows. A record's history is
  // authorized by the same verb, so it is offered by it too.
  canRetrieve: boolean
  // canExport and canImport are the panel's data transfer. The server asks
  // them of the whole panel (export_data and import_data on admin:*), not of
  // the model: they are grants of their own, and holding create does not
  // make an import any less refused. Nor does import_data make a row any
  // less refused: each row of a file asks the model's create, or its update
  // for a row that exists (OR-67), and the dialog's import creates every
  // row it reads — so it is offered to an operator who holds both.
  canExport: boolean
  canImport: boolean
  // canConfigureFields is the field settings dialog (update_schema). It is
  // also false where the data source has no schema registry to change.
  canConfigureFields: boolean
  // True when at least one verb is granted over the operator's OWN rows only,
  // which is worth saying on screen: the list is not the whole table.
  rowScoped: boolean
}

// holds reads one verb from a payload's permission map. A backend that
// sends no map — an older one, or the open posture — leaves it undefined,
// and the fallback says what the screen did before the verb was read.
export function holds(hints: CapabilityHints, verb: string, fallback = true): boolean {
  const held = hints.permissions?.[verb]
  return held === undefined ? fallback : held
}

export function screenCapabilities(schema: ModelSchema): ScreenCapabilities {
  const readOnly = schema.read_only === true
  const canCreate = !readOnly && schema.can_create !== false
  const canDelete = !readOnly && schema.can_delete !== false
  return {
    canCreate,
    canUpdate: !readOnly && schema.can_update !== false,
    canDelete,
    canBulkDelete: !readOnly && holds(schema, 'bulk_delete', canDelete),
    canRetrieve: holds(schema, 'retrieve'),
    // Before the verb was read the export was always offered, and the
    // import was offered with create: those are the fallbacks.
    canExport: holds(schema, 'export_data'),
    canImport: canCreate && holds(schema, 'import_data', canCreate),
    canConfigureFields: holds(schema, 'update_schema'),
    rowScoped: (schema.row_scope?.length ?? 0) > 0,
  }
}

// canOpenModel is whether the sidebar should offer a model at all: opening
// one reads its schema (get_schema) and lists its rows (list), and a model
// the server refuses either of those for opens on an error and nothing else.
export function canOpenModel(model: CapabilityHints): boolean {
  return holds(model, 'get_schema') && holds(model, 'list')
}

// isFieldEditable answers the FIELD question only: whether a policy keeps this
// operator out of this column. Whether they may write the model at all is
// canUpdate, and the two are deliberately separate — folding them together
// drew an empty create form for an operator who may create but not update.
export function isFieldEditable(field: SchemaField): boolean {
  return field.can_edit !== false
}
