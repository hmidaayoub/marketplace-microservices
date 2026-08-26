import { useEffect } from 'react'
import { Link } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../store'
import { fetchMyRequests } from '../store/requestsSlice'
import { Badge, Card, Empty } from '../components/ui'

export default function MyRequests() {
  const dispatch = useAppDispatch()
  const mine = useAppSelector((s) => s.requests.mine)

  useEffect(() => {
    dispatch(fetchMyRequests())
  }, [dispatch])

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">My requests</h1>
      <p className="text-sm text-slate-600">
        Everything you created or joined. Quantities shown are the combined demand, not yours
        alone.
      </p>
      {mine.length === 0 && <Empty>You have not joined a request yet.</Empty>}
      {mine.map((request) => (
        <Link key={request.requestId} to={`/requests/${request.requestId}`}>
          <Card className="flex items-center justify-between transition hover:border-brand-500">
            <div>
              <p className="font-medium">{request.itemName}</p>
              <p className="text-sm text-slate-600">
                {request.totalCustomers} buyers · {request.totalQuantity} units
              </p>
            </div>
            <Badge>{request.status ?? 'OPEN'}</Badge>
          </Card>
        </Link>
      ))}
    </div>
  )
}
