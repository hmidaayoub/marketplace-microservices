/** The audit trail behind the phone-number rule: who holds access, and to what. */

import { useEffect } from 'react'

import { useAppDispatch, useAppSelector } from '../../store'
import { fetchGrants, revokeGrant } from '../../store/adminSlice'
import { Alert, Badge, Button, Card, Empty } from '../../components/ui'

export default function Access() {
  const dispatch = useAppDispatch()
  const { grants, error } = useAppSelector((s) => s.admin)

  useEffect(() => {
    dispatch(fetchGrants())
  }, [dispatch])

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Contact access</h1>
      <p className="text-sm text-slate-600">
        Every grant ever issued. Revoking keeps the record — it marks access withdrawn rather than
        deleting the fact it was given.
      </p>

      <Alert>{error}</Alert>
      {grants.length === 0 && <Empty>No contact access has been granted yet.</Empty>}

      {grants.map((grant) => (
        <Card key={grant.accessId} className="flex items-center justify-between gap-4">
          <div className="text-sm">
            <p>
              <span className="text-slate-500">Seller</span>{' '}
              <span className="font-medium">{grant.sellerId}</span>
            </p>
            <p>
              <span className="text-slate-500">Request</span> {grant.requestId}
            </p>
          </div>
          {grant.status === 'GRANTED' ? (
            <Button variant="danger" onClick={() => dispatch(revokeGrant(grant.accessId!))}>
              Revoke
            </Button>
          ) : (
            // GRANTED | REVOKED | EXPIRED - only a live grant can be revoked, and the
            // row stays either way so the audit trail keeps the fact it was given.
            <Badge>{grant.status ?? 'UNKNOWN'}</Badge>
          )}
        </Card>
      ))}
    </div>
  )
}
