/**
 * The pages pass a nullable error straight in, so the "is there one" check lives here
 * rather than at nine call sites.
 */

import { CircleAlertIcon, InfoIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

export function ErrorAlert({
  children,
  title,
  tone = 'error',
}: {
  children?: string | null | false
  title?: string
  tone?: 'error' | 'info'
}) {
  if (!children) return null

  const Icon = tone === 'error' ? CircleAlertIcon : InfoIcon

  return (
    <Alert variant={tone === 'error' ? 'destructive' : 'default'}>
      <Icon />
      <AlertTitle>{title ?? (tone === 'error' ? 'That did not work' : 'Heads up')}</AlertTitle>
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  )
}
