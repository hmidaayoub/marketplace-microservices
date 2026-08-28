/**
 * One badge for every status the platform has, in one table.
 *
 * The domain's statuses are not success/failure - a PENDING offer is the normal state
 * of a healthy offer, and a request with no buyers on it is not a fault - so they are
 * coloured against the semantic tokens rather than forced into shadcn's three variants.
 */

import {
  BanIcon,
  CircleCheckIcon,
  CircleDotIcon,
  CircleXIcon,
  ClockIcon,
  UsersRoundIcon,
  type LucideIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

interface StatusStyle {
  label: string
  className: string
  icon: LucideIcon
}

const STATUSES: Record<string, StatusStyle> = {
  // Requests
  OPEN: { label: 'Open', className: 'bg-success/12 text-success', icon: CircleDotIcon },
  // Not a terminal state and not a fault: nobody is on this request at the moment, and
  // one join makes it Open again. Muted rather than red for exactly that reason.
  INACTIVE: {
    label: 'No buyers',
    className: 'bg-muted text-muted-foreground',
    icon: UsersRoundIcon,
  },
  // Offers
  PENDING: { label: 'Pending review', className: 'bg-warning/15 text-warning', icon: ClockIcon },
  APPROVED: { label: 'Approved', className: 'bg-success/12 text-success', icon: CircleCheckIcon },
  REJECTED: { label: 'Rejected', className: 'bg-destructive/10 text-destructive', icon: CircleXIcon },
  // Contact access
  GRANTED: { label: 'Granted', className: 'bg-success/12 text-success', icon: CircleCheckIcon },
  REVOKED: { label: 'Revoked', className: 'bg-destructive/10 text-destructive', icon: BanIcon },
  EXPIRED: { label: 'Expired', className: 'bg-muted text-muted-foreground', icon: ClockIcon },
}

export function StatusBadge({ status, className }: { status?: string | null; className?: string }) {
  const key = status ?? 'UNKNOWN'
  const style = STATUSES[key]

  if (!style) {
    return (
      <Badge variant="outline" className={className}>
        {key}
      </Badge>
    )
  }

  const Icon = style.icon
  return (
    <Badge className={cn(style.className, className)} title={key}>
      <Icon />
      {style.label}
    </Badge>
  )
}
