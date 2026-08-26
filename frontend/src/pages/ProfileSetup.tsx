/**
 * Step two of signing up.
 *
 * The platform separates the account from the role profile: auth-service owns identity,
 * customer-service and seller-service own the profile that request- and offer-service
 * resolve a caller through. Until this form is submitted every business write answers
 * 403, so the route guard sends people here rather than letting them find out.
 */

import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'

import { api, ApiError } from '../api/client'
import { useAppDispatch, useAppSelector } from '../store'
import { profileCreated } from '../store/authSlice'
import { Alert, Button, Card, Field, Input } from '../components/ui'

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
      await api(isSeller ? '/api/sellers' : '/api/customers', {
        method: 'POST',
        body,
        token: accessToken,
      })
      dispatch(profileCreated())
      navigate('/requests')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not save your profile')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto mt-12 max-w-md">
      <h1 className="mb-2 text-2xl font-semibold">
        {isSeller ? 'Set up your store' : 'Complete your profile'}
      </h1>
      <p className="mb-6 text-sm text-slate-600">
        Your account exists, but {isSeller ? 'offers' : 'requests'} are made by a profile. This is
        the last step.
      </p>
      <Card>
        <form onSubmit={submit} className="space-y-4">
          {isSeller ? (
            <>
              <Field label="Store name">
                <Input value={form.storeName} onChange={set('storeName')} required />
              </Field>
              <Field label="What you supply">
                <Input value={form.description} onChange={set('description')} required />
              </Field>
              <Field label="City">
                <Input value={form.city} onChange={set('city')} required />
              </Field>
              <Field label="Address">
                <Input value={form.address} onChange={set('address')} required />
              </Field>
              <p className="text-xs text-slate-500">
                Your address stays private — buyers see only your store name and city.
              </p>
            </>
          ) : (
            <>
              <Field label="First name">
                <Input value={form.firstName} onChange={set('firstName')} required />
              </Field>
              <Field label="Last name">
                <Input value={form.lastName} onChange={set('lastName')} required />
              </Field>
            </>
          )}
          <Alert>{error}</Alert>
          <Button type="submit" className="w-full" disabled={saving}>
            {saving ? 'Saving…' : 'Continue'}
          </Button>
        </form>
      </Card>
    </div>
  )
}
