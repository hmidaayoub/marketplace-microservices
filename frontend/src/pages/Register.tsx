import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import type { Role } from '../api/types'
import { useAppDispatch, useAppSelector } from '../store'
import { loadSession, register } from '../store/authSlice'
import { Alert, Button, Card, Field, Input, Select } from '../components/ui'

export default function Register() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const { status, error } = useAppSelector((s) => s.auth)
  const [form, setForm] = useState({
    email: '',
    password: '',
    phoneNumber: '',
    role: 'CUSTOMER' as Role,
  })

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [key]: event.target.value }))

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const result = await dispatch(register(form))
    if (register.fulfilled.match(result)) {
      await dispatch(loadSession())
      // Always /profile: an account exists but its role profile does not, and every
      // business endpoint refuses until it does.
      navigate('/profile')
    }
  }

  return (
    <div className="mx-auto mt-16 max-w-sm">
      <h1 className="mb-6 text-center text-2xl font-semibold">Create an account</h1>
      <Card>
        <form onSubmit={submit} className="space-y-4">
          <Field label="I am a">
            <Select value={form.role} onChange={set('role')}>
              <option value="CUSTOMER">Customer — I want to buy</option>
              <option value="SELLER">Seller — I want to supply</option>
            </Select>
          </Field>
          <Field label="Email">
            <Input type="email" value={form.email} onChange={set('email')} required />
          </Field>
          <Field label="Password">
            <Input
              type="password"
              value={form.password}
              onChange={set('password')}
              required
              minLength={8}
            />
          </Field>
          <Field label="Phone number">
            <Input
              value={form.phoneNumber}
              onChange={set('phoneNumber')}
              placeholder="+216 00 000 000"
              required
            />
          </Field>
          <p className="text-xs text-slate-500">
            Digits and an optional leading +, and it must not already be registered.
            Your phone number never leaves the auth service. A seller sees it only after an
            administrator approves their offer on a request you joined.
          </p>
          <Alert>{error}</Alert>
          {error?.includes('already registered') && (
            <Link
              to="/login"
              className="block text-center text-sm font-medium text-brand-600 hover:underline"
            >
              Go to sign in →
            </Link>
          )}
          <Button type="submit" className="w-full" disabled={status === 'loading'}>
            {status === 'loading' ? 'Creating…' : 'Create account'}
          </Button>
        </form>
      </Card>
      <p className="mt-4 text-center text-sm text-slate-600">
        Already registered?{' '}
        <Link to="/login" className="font-medium text-brand-600 hover:underline">
          Sign in
        </Link>
      </p>
    </div>
  )
}
