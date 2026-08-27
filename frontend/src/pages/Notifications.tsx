/**
 * The inbox.
 *
 * These arrive over RabbitMQ: a producer writes the event to its outbox inside the
 * business transaction and a relay publishes it on a two-second tick. So an action's
 * notification does not exist the instant the action returns - the list refreshes on a
 * timer rather than pretending otherwise.
 */

import { useEffect } from 'react'
import { BellIcon, CheckIcon } from 'lucide-react'

import { PageHeader } from '@/components/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { useAppDispatch, useAppSelector } from '@/store'
import { fetchNotifications, fetchUnreadCount, markRead } from '@/store/notificationsSlice'

const REFRESH_MS = 10_000

export default function Notifications() {
  const dispatch = useAppDispatch()
  const { items, loading, unread } = useAppSelector((s) => s.notifications)

  useEffect(() => {
    dispatch(fetchNotifications())
    dispatch(fetchUnreadCount())
    const id = setInterval(() => dispatch(fetchNotifications()), REFRESH_MS)
    return () => clearInterval(id)
  }, [dispatch])

  return (
    <div className="space-y-6">
      <PageHeader
        title="Notifications"
        description="Events reach you a couple of seconds after they happen — producers publish through an outbox, so this list refreshes on a timer."
      >
        {unread > 0 && (
          <Badge variant="destructive" className="tabular-nums">
            {unread} unread
          </Badge>
        )}
      </PageHeader>

      {loading && items.length === 0 && (
        <div className="space-y-3">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full rounded-xl" />
          ))}
        </div>
      )}

      {!loading && items.length === 0 && (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BellIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing yet</EmptyTitle>
            <EmptyDescription>
              Joining a request, an offer on one you are part of, or an admin decision all land
              here.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      <div className="space-y-3">
        {items.map((notification) => {
          const isUnread = notification.status !== 'READ'
          return (
            <Card
              key={notification.notificationId}
              size="sm"
              className={cn(isUnread && 'bg-accent/40 ring-primary/25')}
            >
              <CardContent className="flex items-start justify-between gap-4">
                <div className="min-w-0 space-y-0.5">
                  <p className="flex items-center gap-2 font-medium">
                    {isUnread && (
                      <span className="size-1.5 shrink-0 rounded-full bg-primary" aria-hidden />
                    )}
                    {notification.title}
                  </p>
                  <p className="text-sm text-muted-foreground">{notification.message}</p>
                  <p className="text-xs text-muted-foreground/80">
                    {notification.type}
                    {notification.createdAt
                      ? ` · ${new Date(notification.createdAt).toLocaleString()}`
                      : ''}
                  </p>
                </div>

                {isUnread && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => dispatch(markRead(notification.notificationId!))}
                  >
                    <CheckIcon />
                    Mark read
                  </Button>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>
    </div>
  )
}
