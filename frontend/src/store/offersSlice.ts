/** Seller offers against the aggregated demand of a request. */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api, ApiError } from '@/api/client'
import type { AnyOffer, Offer, OfferCreate, OfferUpdate } from '@/api/types'
import type { RootState } from './index'

/**
 * What a refused create carries back: why, and the offer to change instead.
 *
 * A seller answers a request once, so submitting a second offer against demand they
 * have already bid on is refused with the first one attached - the same shape a refused
 * request create uses, for the same reason: "you cannot do this" is only half an answer
 * without what to do instead.
 */
interface CreateRejection {
  message: string
  existing: Offer | null
}

function existingIn(error: unknown): Offer | null {
  if (!(error instanceof ApiError) || typeof error.body !== 'object' || error.body === null)
    return null
  const { existing } = error.body as { existing?: Offer }
  return existing ?? null
}

interface OffersState {
  mine: Offer[]
  forRequest: AnyOffer[]
  loading: boolean
  error: string | null
}

const initialState: OffersState = { mine: [], forRequest: [], loading: false, error: null }

const tokenOf = (state: RootState) => state.auth.accessToken

export const fetchMyOffers = createAsyncThunk<Offer[], void, { state: RootState }>(
  'offers/fetchMine',
  async (_, { getState }) => api('/api/offers/me', { token: tokenOf(getState()) }),
)

export const fetchOffersForRequest = createAsyncThunk<AnyOffer[], string, { state: RootState }>(
  'offers/fetchForRequest',
  async (requestId, { getState }) =>
    api(`/api/offers/request/${requestId}`, { token: tokenOf(getState()) }),
)

export const createOffer = createAsyncThunk<
  Offer,
  OfferCreate,
  { state: RootState; rejectValue: CreateRejection }
>('offers/create', async (body, { getState, rejectWithValue }) => {
  try {
    return await api<Offer>('/api/offers', { method: 'POST', body, token: tokenOf(getState()) })
  } catch (error) {
    return rejectWithValue({
      message: error instanceof ApiError ? error.message : 'Could not submit offer',
      existing: existingIn(error),
    })
  }
})

/** Changing the terms of an offer already made. Only a PENDING offer accepts this. */
export const updateOffer = createAsyncThunk<
  Offer,
  { offerId: string; body: OfferUpdate },
  { state: RootState; rejectValue: string }
>('offers/update', async ({ offerId, body }, { getState, rejectWithValue }) => {
  try {
    return await api<Offer>(`/api/offers/${offerId}`, {
      method: 'PUT',
      body,
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not update offer')
  }
})

/** Withdrawing one. A status change, not a delete: the record survives for the audit
 *  history, and withdrawing frees the seller to offer on that request again. */
export const cancelOffer = createAsyncThunk<
  string,
  string,
  { state: RootState; rejectValue: string }
>('offers/cancel', async (offerId, { getState, rejectWithValue }) => {
  try {
    await api<void>(`/api/offers/${offerId}`, { method: 'DELETE', token: tokenOf(getState()) })
    return offerId
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not cancel offer')
  }
})

const offersSlice = createSlice({
  name: 'offers',
  initialState,
  reducers: {},
  extraReducers: (builder) => {
    builder
      .addCase(fetchMyOffers.pending, (state) => {
        state.loading = true
      })
      .addCase(fetchMyOffers.fulfilled, (state, action) => {
        state.loading = false
        state.mine = action.payload
      })
      .addCase(fetchOffersForRequest.fulfilled, (state, action) => {
        state.forRequest = action.payload
      })
      .addCase(createOffer.fulfilled, (state, action) => {
        state.mine.unshift(action.payload)
        state.error = null
      })
      .addCase(updateOffer.fulfilled, (state, action) => {
        const replace = (offer: AnyOffer) =>
          offer.offerId === action.payload.offerId ? action.payload : offer
        state.mine = state.mine.map(replace) as Offer[]
        state.forRequest = state.forRequest.map(replace)
        state.error = null
      })
      .addCase(cancelOffer.fulfilled, (state, action) => {
        // Cancelled offers stay on the record but drop out of what the seller is shown:
        // there is nothing left to do with one, and it is no longer their answer to
        // that request.
        state.mine = state.mine.filter((offer) => offer.offerId !== action.payload)
        state.forRequest = state.forRequest.filter((offer) => offer.offerId !== action.payload)
        state.error = null
      })
      .addMatcher(
        (action): action is { type: string; payload?: string | CreateRejection } =>
          action.type.startsWith('offers/') && action.type.endsWith('/rejected'),
        (state, action) => {
          state.loading = false
          const payload = action.payload
          state.error =
            (typeof payload === 'string' ? payload : payload?.message) ?? 'Something went wrong'
        },
      )
  },
})

export default offersSlice.reducer
