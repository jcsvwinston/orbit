// Full-screen not-authorized state (OR-UX-P1-5). When any RPC returns
// Unauthenticated, App swaps the whole shell for this instead of every
// page rendering its own raw "network error". It explains the three
// deployment auth paths so an operator knows why they're locked out.
import { t } from '@/lib/i18n'

export function NotAuthorizedPage(props: { onRetry: () => void }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-t0 p-6 font-sans text-t46">
      <div className="w-full max-w-[520px] rounded-[12px] border border-t18 bg-t5 p-7">
        <div className="mb-2 flex items-center gap-2.5">
          <span className="h-[9px] w-[9px] rounded-full" style={{ background: 'var(--t51)' }} />
          <h1 className="m-0 text-[18px] font-semibold text-t46">{t.notAuthorized.title}</h1>
        </div>
        <p className="mb-4 mt-0 text-[13px] leading-relaxed text-t33">
          {t.notAuthorized.intro}
        </p>
        <ul className="mb-5 mt-0 flex list-none flex-col gap-2 p-0 text-[12.5px] text-t35">
          <li className="rounded-[8px] border border-t14 bg-t6 px-3 py-2.5">
            <span className="font-semibold text-t42">{t.notAuthorized.reverseProxyTitle}</span> — an
            oauth2-proxy / SSO layer that forwards{' '}
            <code className="font-mono text-t39">X-Auth-User</code> (and, when
            configured, <code className="font-mono text-t39">X-Auth-Proxy-Secret</code>)
            from a trusted CIDR.
          </li>
          <li className="rounded-[8px] border border-t14 bg-t6 px-3 py-2.5">
            <span className="font-semibold text-t42">{t.notAuthorized.bearerTitle}</span> — the
            server started with <code className="font-mono text-t39">--ui-bearer</code>;
            send <code className="font-mono text-t39">Authorization: Bearer …</code>.
          </li>
          <li className="rounded-[8px] border border-t14 bg-t6 px-3 py-2.5">
            <span className="font-semibold text-t42">{t.notAuthorized.localTitle}</span> — reach
            the listener from a trusted CIDR (loopback by default).
          </li>
        </ul>
        <button
          type="button"
          onClick={props.onRetry}
          className="rounded-[7px] px-3 py-1.5 text-[12.5px] font-semibold text-t53 transition-[filter] hover:brightness-110"
          style={{ background: 'var(--accent)' }}
        >
          {t.notAuthorized.retry}
        </button>
      </div>
    </div>
  )
}
