import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../store'
import { loadSession, login } from '../store/authSlice'
import { Alert, Button, Card, Field, Input } from '../components/ui'

export default function Login() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const { status, error } = useAppSelector((s) => s.auth)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

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
    <div className="mx-auto mt-16 max-w-sm">
      <h1 className="mb-6 text-center text-2xl font-semibold">Sign in</h1>
      <Card>
        <form onSubmit={submit} className="space-y-4">
          <Field label="Email">
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              autoComplete="email"
            />
          </Field>
          <Field label="Password">
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              autoComplete="current-password"
            />
          </Field>
          <Alert>{error}</Alert>
          <Button type="submit" className="w-full" disabled={status === 'loading'}>
            {status === 'loading' ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
      </Card>
      <p className="mt-4 text-center text-sm text-slate-600">
        No account?{' '}
        <Link to="/register" className="font-medium text-brand-600 hover:underline">
          Register
        </Link>
      </p>
    </div>
  )
}
