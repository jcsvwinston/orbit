// describeOperator is the footer line that says who the operator's actions
// are audited as: the subject, "(viewer)" when read-only, and the tenant the
// server scoped them to when there is one. The tenant comes from GetSelf
// (SelfInfo.tenant), which the server fills from its trusted proxy's tenant
// header; a server that predates the field leaves it empty and the line
// shows only the subject.

import type { SelfInfo } from '@/fleet/gen/nucleus/admin/v1/admin_pb'

import { t } from './i18n'

export function describeOperator(self: SelfInfo | undefined): string {
  if (!self?.subject) return ''
  const parts = [`${self.subject}${self.readOnly ? t.app.viewerSuffix : ''}`]
  if (self.tenant) parts.push(t.app.tenantLabel(self.tenant))
  return parts.join(' · ')
}
