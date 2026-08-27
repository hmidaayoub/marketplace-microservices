/**
 * Step two of signing up.
 *
 * The platform separates the account from the role profile: auth-service owns identity,
 * customer-service and seller-service own the profile that request- and offer-service
 * resolve a caller through. Until this form is submitted every business write answers
 * 403, so the route guard sends people here rather than letting them find out.
 */

import { useState } from 'react'
import { StoreIcon, UserRoundIcon } from 'lucide-react'
import { Navigate, useNavigate } from 'react-router-dom'

import { AuthShell } from '@/components/auth-shell'
import { ErrorAlert } from '@/components/error-alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError } from '@/api/client'
import type { Customer, Seller } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { profileCreated } from '@/store/authSlice'

export default function ProfileSetup() {
  const dispatch = useAppDispatch()
  const navigate = useNavigate()
  const { user, accessToken, hasProfile } = useAppSelector((s) => s.auth)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const isSeller = user?.role === 'SELLER'
  const [form, setForm] = useState({
    firstName: '',
    lastName: '',
    storeName: '',
    description: '',
    city: '',
    address: '',
  })

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [key]: event.target.value }))

  // Declarative, not a navigate() call: navigating during render updates the router
  // while this component is still rendering, which React reports as a bad setState.
  if (hasProfile) return <Navigate to="/requests" replace />

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setError(null)
    const body = isSeller
      ? {
          storeName: form.storeName,
          description: form.description,
          city: form.city,
          address: form.address,
        }
      : { firstName: form.firstName, lastName: form.lastName }
    try {
      // The response is the created profile, and it is the only place the account's name
      // lives - keeping it saves a refetch just to greet them by it.
      const profile = await api<Customer | Seller>(isSeller ? '/api/sellers' : '/api/customers', {
        method: 'POST',
        body,
        token: accessToken,
      })
      dispatch(profileCreated(profile))
      navigate('/requests')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not save your profile')
    } finally {
      setSaving(false)
    }
  }

  return (
    <AuthShell
      icon={isSeller ? StoreIcon : UserRoundIcon}
      title={isSeller ? 'Set up your store' : 'Complete your profile'}
      description={`Your account exists, but ${
        isSeller ? 'offers' : 'requests'
      } are made by a profile. This is the last step.`}
    >
      <form onSubmit={submit}>
        <FieldGroup>
          {isSeller ? (
            <>
              <Field>
                <FieldLabel htmlFor="storeName">Store name</FieldLabel>
                <Input id="storeName" value={form.storeName} onChange={set('storeName')} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="description">What you supply</FieldLabel>
                <Textarea
                  id="description"
                  value={form.description}
                  onChange={set('description')}
                  rows={3}
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="city">City</FieldLabel>
                <Input id="city" value={form.city} onChange={set('city')} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="address">Address</FieldLabel>
                <Input id="address" value={form.address} onChange={set('address')} required />
                <FieldDescription>
                  Your address stays private — buyers see only your store name and city.
                </FieldDescription>
              </Field>
            </>
          ) : (
            <>
              <Field>
                <FieldLabel htmlFor="firstName">First name</FieldLabel>
                <Input id="firstName" value={form.firstName} onChange={set('firstName')} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="lastName">Last name</FieldLabel>
                <Input id="lastName" value={form.lastName} onChange={set('lastName')} required />
              </Field>
            </>
          )}

          <ErrorAlert title="Could not save your profile">{error}</ErrorAlert>

          <Button type="submit" size="lg" className="w-full" disabled={saving}>
            {saving && <Spinner />}
            {saving ? 'Saving…' : 'Continue'}
          </Button>
        </FieldGroup>
      </form>
    </AuthShell>
  )
}
