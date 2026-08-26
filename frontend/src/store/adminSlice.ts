/**
 * The admin queue and the contact access a decision grants.
 *
 * Approving is the single gate in the whole platform: it is what turns a seller's offer
 * into permission to see the customers' phone numbers on that request. Nothing else in
 * the system unlocks a phone number.
 */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api, ApiError } from '../api/client'
import type { ContactAccess, ContactList, Decision, PendingOffer } from '../api/types'
import type { RootState } from './index'

interface AdminState {
  pending: PendingOffer[]
  grants: ContactAccess[]
  contacts: ContactList | null
  loading: boolean
  error: string | null
}

const initialState: AdminState = {
  pending: [],
  grants: [],
  contacts: null,
  loading: false,
  error: null,
}

const tokenOf = (state: RootState) => state.auth.accessToken

export const fetchPending = createAsyncThunk<PendingOffer[], void, { state: RootState }>(
  'admin/fetchPending',
  async (_, { getState }) => api('/api/admin/offers/pending', { token: tokenOf(getState()) }),
)

export const decideOffer = createAsyncThunk<
  { offerId: string; decision: Decision },
  { offerId: string; approve: boolean; reason: string },
  { state: RootState; rejectValue: string }
>('admin/decide', async ({ offerId, approve, reason }, { getState, rejectWithValue }) => {
  const verb = approve ? 'approve' : 'reject'
  try {
    const decision = await api<Decision>(`/api/admin/offers/${offerId}/${verb}`, {
      method: 'POST',
      body: { reason },
      token: tokenOf(getState()),
    })
    return { offerId, decision }
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Decision failed')
  }
})

export const fetchGrants = createAsyncThunk<ContactAccess[], string | void, { state: RootState }>(
  'admin/fetchGrants',
  async (requestId, { getState }) => {
    const query = requestId ? `?requestId=${requestId}` : ''
    return api(`/api/admin/contact-access${query}`, { token: tokenOf(getState()) })
  },
)

export const revokeGrant = createAsyncThunk<
  string,
  string,
  { state: RootState; rejectValue: string }
>('admin/revoke', async (accessId, { getState, rejectWithValue }) => {
  try {
    await api(`/api/admin/contact-access/${accessId}`, {
      method: 'DELETE',
      token: tokenOf(getState()),
    })
    return accessId
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not revoke')
  }
})

/** The seller's own view: granted contacts for one request, phone numbers included. */
export const fetchContacts = createAsyncThunk<
  ContactList,
  string,
  { state: RootState; rejectValue: string }
>('admin/fetchContacts', async (requestId, { getState, rejectWithValue }) => {
  try {
    return await api<ContactList>(`/api/contacts/requests/${requestId}`, {
      token: tokenOf(getState()),
    })
  } catch (error) {
    // 403 here is the normal state before approval, not a fault.
    return rejectWithValue(
      error instanceof ApiError && error.status === 403
        ? 'No contact access has been granted for this request yet.'
        : 'Could not load contacts',
    )
  }
})

const adminSlice = createSlice({
  name: 'admin',
  initialState,
  reducers: {
    contactsCleared(state) {
      state.contacts = null
      state.error = null
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchPending.pending, (state) => {
        state.loading = true
      })
      .addCase(fetchPending.fulfilled, (state, action) => {
        state.loading = false
        state.pending = action.payload
      })
      .addCase(decideOffer.fulfilled, (state, action) => {
        // Decided offers leave the queue; the list is the queue, so drop it locally
        // rather than refetching just to see one row disappear.
        state.pending = state.pending.filter((o) => o.offerId !== action.payload.offerId)
        state.error = null
      })
      .addCase(fetchGrants.fulfilled, (state, action) => {
        state.grants = action.payload
      })
      .addCase(revokeGrant.fulfilled, (state, action) => {
        state.grants = state.grants.map((g) =>
          g.accessId === action.payload ? { ...g, status: 'REVOKED' } : g,
        )
      })
      .addCase(fetchContacts.fulfilled, (state, action) => {
        state.contacts = action.payload
        state.error = null
      })
      .addMatcher(
        (action): action is { type: string; payload?: string } =>
          action.type.startsWith('admin/') && action.type.endsWith('/rejected'),
        (state, action) => {
          state.loading = false
          state.error = action.payload ?? 'Something went wrong'
        },
      )
  },
})

export const { contactsCleared } = adminSlice.actions
export default adminSlice.reducer
