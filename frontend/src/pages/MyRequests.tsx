import { useEffect } from 'react'
import { ChevronRightIcon, ScrollTextIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { useAppDispatch, useAppSelector } from '@/store'
import { fetchMyRequests } from '@/store/requestsSlice'

export default function MyRequests() {
  const dispatch = useAppDispatch()
  const mine = useAppSelector((s) => s.requests.mine)

  useEffect(() => {
    dispatch(fetchMyRequests())
  }, [dispatch])

  return (
    <div className="space-y-6">
      <PageHeader
        title="My requests"
        description="Everything you created or joined. Quantities shown are the combined demand, not yours alone."
      />

      {mine.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ScrollTextIcon />
            </EmptyMedia>
            <EmptyTitle>You have not joined a request yet</EmptyTitle>
            <EmptyDescription>
              Joining an open request adds your quantity to its total, which is what sellers bid
              against.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild>
              <Link to="/requests">Browse open demand</Link>
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <div className="space-y-3">
          {mine.map((request) => (
            <Link
              key={request.requestId}
              to={`/requests/${request.requestId}`}
              className="group block rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <Card size="sm" className="transition-shadow group-hover:ring-primary/40">
                <CardContent className="flex items-center justify-between gap-4">
                  <div className="min-w-0">
                    <p className="font-medium">{request.itemName}</p>
                    <p className="text-sm text-muted-foreground tabular-nums">
                      {request.totalCustomers} buyers · {request.totalQuantity} units
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <StatusBadge status={request.status ?? 'OPEN'} />
                    <ChevronRightIcon className="size-4 text-muted-foreground" />
                  </div>
                </CardContent>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}
