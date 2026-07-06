// Page scaffolding (design handoff): header 17px/28px padding with bottom
// border; H1 17/600 + 12.5px muted description; contextual actions right.
// Body padding 22px 28px.
import { type ReactNode } from 'react'

export function PageHeader(props: { title: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="flex items-center justify-between gap-4 border-b border-t14 px-7 py-[17px]">
      <div className="min-w-0">
        <h1 className="m-0 text-[17px] font-semibold text-t46">{props.title}</h1>
        {props.description !== undefined && (
          <p className="mb-0 mt-[3px] text-[12.5px] text-t30">{props.description}</p>
        )}
      </div>
      {props.actions !== undefined && (
        <div className="flex shrink-0 items-center gap-2.5">{props.actions}</div>
      )}
    </header>
  )
}

export function PageBody(props: { children: ReactNode; className?: string }) {
  return <div className={['px-7 py-[22px]', props.className ?? ''].join(' ')}>{props.children}</div>
}
