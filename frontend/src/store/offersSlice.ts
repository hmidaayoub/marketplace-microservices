/** Seller offers against the aggregated demand of a request. */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api, ApiError } from '../api/client'
import type { AnyOffer, Offer, OfferCreate } from '../api/types'
import type { RootState } from './index'

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
  { state: RootState; rejectValue: string }
>('offers/create', async (body, { getState, rejectWithValue }) => {
  try {
    return await api<Offer>('/api/offers', { method: 'POST', body, token: tokenOf(getState()) })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not submit offer')
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
      .addMatcher(
        (action): action is { type: string; payload?: string } =>
          action.type.startsWith('offers/') && action.type.endsWith('/rejected'),
        (state, action) => {
          state.loading = false
          state.error = action.payload ?? 'Something went wrong'
        },
      )
  },
})

export default offersSlice.reducer
