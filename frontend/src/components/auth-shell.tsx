/** The centred card the three signed-out (or half-signed-in) forms share. */

import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { ModeToggle } from '@/components/mode-toggle'
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card'

export function AuthShell({
  icon: Icon,
  title,
  description,
  children,
  footer,
}: {
  icon: LucideIcon
  title: string
  description: ReactNode
  children: ReactNode
  footer?: ReactNode
}) {
  return (
    <div className="relative flex min-h-svh flex-col items-center justify-center gap-6 bg-muted/40 px-4 py-12">
      <div className="absolute top-4 right-4">
        <ModeToggle />
      </div>

      <Card className="w-full max-w-md [--card-spacing:--spacing(6)]">
        <CardHeader className="justify-items-center text-center">
          <span className="mb-1 flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Icon className="size-4.5" />
          </span>
          {/* A real <h1>: it is the page's only heading, and the e2e run finds pages by it. */}
          <h1 className="font-heading text-xl font-semibold tracking-tight">{title}</h1>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>

      {footer && <p className="text-sm text-muted-foreground">{footer}</p>}
    </div>
  )
}
