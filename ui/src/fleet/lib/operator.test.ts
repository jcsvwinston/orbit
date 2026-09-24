import { describe, expect, it } from 'vitest'
import { create } from '@bufbuild/protobuf'

import { SelfInfoSchema } from '@/fleet/gen/nucleus/admin/v1/admin_pb'
import { describeOperator } from './operator'

describe('describeOperator', () => {
  it('names the subject, the viewer role and the tenant the server scoped the operator to', () => {
    expect(describeOperator(undefined)).toBe('')
    expect(describeOperator(create(SelfInfoSchema, { subject: 'alice' }))).toBe('alice')
    expect(describeOperator(create(SelfInfoSchema, { subject: 'alice', readOnly: true }))).toBe('alice (viewer)')
    expect(describeOperator(create(SelfInfoSchema, { subject: 'alice', tenant: 'acme' }))).toBe('alice · tenant acme')
  })
})
