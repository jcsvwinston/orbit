import type { ModelActionSpec } from '@/types'

// Where an application action is offered (EXT-02). The server enforces the
// placement — an action is refused from a place it is not offered — so these
// only decide where a button is drawn, never what is allowed.

// offeredOnSelection: the action belongs on the grid's toolbar, over the
// rows the operator selected. An action with no placement is one from a
// server older than placements, where every action was a selection action.
export function offeredOnSelection(action: ModelActionSpec): boolean {
  return !action.placement || action.placement === 'selection' || action.placement === 'selection_and_record'
}

// offeredOnRecord: the action belongs on one record — its record view and
// its row's menu.
export function offeredOnRecord(action: ModelActionSpec): boolean {
  return action.placement === 'record' || action.placement === 'selection_and_record'
}
