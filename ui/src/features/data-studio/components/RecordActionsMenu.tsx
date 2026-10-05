import { Menu } from '@base-ui/react/menu'
import { MoreHorizontal } from 'lucide-react'
import type { ModelActionSpec } from '@/types'

interface Props {
  // actions are the ones offered on one record (EXT-02) that this operator
  // may run; the schema already left out the others.
  actions: ModelActionSpec[]
  // recordLabel names the record in the trigger's accessible name.
  recordLabel: string
  disabled?: boolean
  onSelect: (action: ModelActionSpec) => void
}

// RecordActionsMenu is a row's own menu of application actions: the way to
// run an action on one record without opening it, and the way an operator
// who may run the action but not edit the record reaches it at all — the
// record view is the edit form, which they are not offered.
export default function RecordActionsMenu({ actions, recordLabel, disabled, onSelect }: Props) {
  return (
    <Menu.Root>
      <Menu.Trigger
        disabled={disabled}
        title="Actions"
        aria-label={`Actions for record ${recordLabel}`}
        className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <MoreHorizontal className="h-3.5 w-3.5" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={4} align="end" className="z-50">
          <Menu.Popup className="min-w-[10rem] rounded-md border bg-popover p-1 text-popover-foreground shadow-md focus:outline-none">
            {actions.map((action) => (
              <Menu.Item
                key={action.name}
                title={action.description}
                onClick={() => onSelect(action)}
                className="flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-muted data-[highlighted]:text-foreground"
              >
                {action.label}
              </Menu.Item>
            ))}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
