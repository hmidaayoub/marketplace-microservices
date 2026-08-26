/**
 * The inbox.
 *
 * These arrive over RabbitMQ: a producer writes the event to its outbox inside the
 * business transaction and a relay publishes it on a two-second tick. So an action's
 * notification does not exist the instant the action returns - the list refreshes on a
 * timer rather than pretending otherwise.
 */

import { useEffect } from 'react'

import { useAppDispatch, useAppSelector } from '../store'
import { fetchNotifications, fetchUnreadCount, markRead } from '../store/notificationsSlice'
import { Button, Card, Empty } from '../components/ui'

const REFRESH_MS = 10_000

export default function Notifications() {
  const dispatch = useAppDispatch()
  const { items, loading } = useAppSelector((s) => s.notifications)

  useEffect(() => {
    dispatch(fetchNotifications())
    dispatch(fetchUnreadCount())
    const id = setInterval(() => dispatch(fetchNotifications()), REFRESH_MS)
    return () => clearInterval(id)
  }, [dispatch])

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Notifications</h1>
      {loading && items.length === 0 && <Empty>Loading…</Empty>}
      {!loading && items.length === 0 && <Empty>Nothing yet.</Empty>}

      {items.map((notification) => {
        const unread = notification.status !== 'READ'
        return (
          <Card
            key={notification.notificationId}
            className={`flex items-start justify-between gap-4 ${unread ? 'border-brand-100 bg-brand-50/40' : ''}`}
          >
            <div>
              <p className="font-medium">{notification.title}</p>
              <p className="text-sm text-slate-600">{notification.message}</p>
              <p className="mt-1 text-xs text-slate-400">
                {notification.type}
                {notification.createdAt ? ` · ${new Date(notification.createdAt).toLocaleString()}` : ''}
              </p>
            </div>
            {unread && (
              <Button
                variant="ghost"
                onClick={() => dispatch(markRead(notification.notificationId!))}
              >
                Mark read
              </Button>
            )}
          </Card>
        )
      })}
    </div>
  )
}
