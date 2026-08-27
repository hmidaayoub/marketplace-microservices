import { useState } from 'react'
import { LogInIcon } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'

import { AuthShell } from '@/components/auth-shell'
import { ErrorAlert } from '@/components/error-alert'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { useAppDispatch, useAppSelector } from '@/store'
import { loadSession, login } from '@/store/authSlice'

export default function Login() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const { status, error } = useAppSelector((s) => s.auth)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  const busy = status === 'loading'

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const result = await dispatch(login({ email, password }))
    if (login.fulfilled.match(result)) {
      // The profile check rides along with the session load, so the guard knows where
      // to send this account before the first page renders.
      await dispatch(loadSession())
      navigate('/requests')
    }
  }

  return (
    <AuthShell
      icon={LogInIcon}
      title="Sign in"
      description="Buyers pool demand, sellers bid against the total."
      footer={
        <>
          No account?{' '}
          <Link to="/register" className="font-medium text-primary hover:underline">
            Register
          </Link>
        </>
      }
    >
      <form onSubmit={submit}>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="email">Email</FieldLabel>
            <Input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoComplete="email"
              placeholder="you@example.com"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="password">Password</FieldLabel>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoComplete="current-password"
            />
          </Field>

          <ErrorAlert title="Could not sign in">{error}</ErrorAlert>

          <Button type="submit" size="lg" className="w-full" disabled={busy}>
            {busy && <Spinner />}
            {busy ? 'Signing in…' : 'Sign in'}
          </Button>
        </FieldGroup>
      </form>
    </AuthShell>
  )
}
