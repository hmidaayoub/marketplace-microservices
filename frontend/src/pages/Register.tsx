import { useState } from 'react'
import { ShoppingBasketIcon, StoreIcon, UserPlusIcon } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'

import { AuthShell } from '@/components/auth-shell'
import { ErrorAlert } from '@/components/error-alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import type { Role } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { loadSession, register } from '@/store/authSlice'

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

  const busy = status === 'loading'
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
    <AuthShell
      icon={UserPlusIcon}
      title="Create an account"
      description="One account, one role — buying or supplying."
      footer={
        <>
          Already registered?{' '}
          <Link to="/login" className="font-medium text-primary hover:underline">
            Sign in
          </Link>
        </>
      }
    >
      <form onSubmit={submit}>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="role">I am a</FieldLabel>
            <Select
              value={form.role}
              onValueChange={(role) => setForm((f) => ({ ...f, role: role as Role }))}
            >
              <SelectTrigger id="role" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="CUSTOMER">
                  <ShoppingBasketIcon />
                  Customer — I want to buy
                </SelectItem>
                <SelectItem value="SELLER">
                  <StoreIcon />
                  Seller — I want to supply
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor="email">Email</FieldLabel>
            <Input
              id="email"
              type="email"
              value={form.email}
              onChange={set('email')}
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
              value={form.password}
              onChange={set('password')}
              required
              minLength={8}
              autoComplete="new-password"
            />
            <FieldDescription>At least 8 characters.</FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="phoneNumber">Phone number</FieldLabel>
            <Input
              id="phoneNumber"
              value={form.phoneNumber}
              onChange={set('phoneNumber')}
              placeholder="+216 00 000 000"
              autoComplete="tel"
              required
            />
            <FieldDescription>
              Digits and an optional leading +, and it must not already be registered. Your phone
              number never leaves the auth service. A seller sees it only after an administrator
              approves their offer on a request you joined.
            </FieldDescription>
          </Field>

          <ErrorAlert title="Could not create the account">{error}</ErrorAlert>

          {error?.includes('already registered') && (
            <Button variant="outline" asChild className="w-full">
              <Link to="/login">Go to sign in</Link>
            </Button>
          )}

          <Button type="submit" size="lg" className="w-full" disabled={busy}>
            {busy && <Spinner />}
            {busy ? 'Creating…' : 'Create account'}
          </Button>
        </FieldGroup>
      </form>
    </AuthShell>
  )
}
