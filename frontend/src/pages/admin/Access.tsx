/** The audit trail behind the phone-number rule: who holds access, and to what. */

import { useEffect } from 'react'
import { BanIcon, KeyIcon } from 'lucide-react'

import { ErrorAlert } from '@/components/error-alert'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useAppDispatch, useAppSelector } from '@/store'
import { fetchGrants, revokeGrant } from '@/store/adminSlice'

export default function Access() {
  const dispatch = useAppDispatch()
  const { grants, error } = useAppSelector((s) => s.admin)

  useEffect(() => {
    dispatch(fetchGrants())
  }, [dispatch])

  return (
    <div className="space-y-6">
      <PageHeader
        title="Contact access"
        description="Every grant ever issued. Revoking keeps the record — it marks access withdrawn rather than deleting the fact it was given."
      />

      <ErrorAlert>{error}</ErrorAlert>

      {grants.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <KeyIcon />
            </EmptyMedia>
            <EmptyTitle>No contact access has been granted yet</EmptyTitle>
            <EmptyDescription>
              A grant appears here the moment an offer is approved in the queue.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Card className="py-0">
          <CardContent className="overflow-x-auto px-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4">Seller</TableHead>
                  <TableHead>Request</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="pr-4 text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {grants.map((grant) => (
                  <TableRow key={grant.accessId}>
                    <TableCell className="pl-4 font-mono text-xs">{grant.sellerId}</TableCell>
                    <TableCell className="font-mono text-xs">{grant.requestId}</TableCell>
                    <TableCell>
                      <StatusBadge status={grant.status} />
                    </TableCell>
                    <TableCell className="pr-4 text-right">
                      {/* GRANTED | REVOKED | EXPIRED - only a live grant can be revoked,
                          and the row stays either way so the audit trail keeps the fact
                          it was given. */}
                      {grant.status === 'GRANTED' && (
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button variant="destructive" size="sm">
                              <BanIcon />
                              Revoke
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>Revoke this access?</AlertDialogTitle>
                              <AlertDialogDescription>
                                The seller stops being able to look the numbers up. It does not
                                undo what they have already seen, and the grant stays on the
                                record as revoked.
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>Cancel</AlertDialogCancel>
                              <AlertDialogAction
                                onClick={() => dispatch(revokeGrant(grant.accessId!))}
                              >
                                Revoke
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
