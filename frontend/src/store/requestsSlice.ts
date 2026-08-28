/** Purchase requests and the caller's participation in them. */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api, ApiError } from '@/api/client'
import type { CreateRequestBody, PurchaseRequest, RequestStatus } from '@/api/types'
import type { RootState } from './index'

interface RequestsState {
  browse: PurchaseRequest[]
  mine: PurchaseRequest[]
  current: PurchaseRequest | null
  /** Open requests whose item name is close to what is being typed into the new-request form. */
  similar: PurchaseRequest[]
  loading: boolean
  error: string | null
}

const initialState: RequestsState = {
  browse: [],
  mine: [],
  current: null,
  similar: [],
  loading: false,
  error: null,
}

/** What a refused create carries back: why, and the open request to join instead. */
interface CreateRejection {
  message: string
  existing: PurchaseRequest | null
}

function existingIn(error: unknown): PurchaseRequest | null {
  if (!(error instanceof ApiError) || typeof error.body !== 'object' || error.body === null) return null
  const { existing } = error.body as { existing?: PurchaseRequest }
  return existing ?? null
}

const tokenOf = (state: unknown) => (state as RootState).auth.accessToken

export const fetchRequests = createAsyncThunk<
  PurchaseRequest[],
  { q?: string; category?: string; status?: RequestStatus } | void,
  { state: RootState }
>('requests/fetch', async (filters, { getState }) => {
  const params = new URLSearchParams()
  if (filters?.q) params.set('q', filters.q)
  if (filters?.category) params.set('category', filters.category)
  if (filters?.status) params.set('status', filters.status)
  const query = params.toString()
  return api<PurchaseRequest[]>(`/api/requests${query ? `?${query}` : ''}`, {
    token: tokenOf(getState()),
  })
})

export const fetchMyRequests = createAsyncThunk<PurchaseRequest[], void, { state: RootState }>(
  'requests/fetchMine',
  async (_, { getState }) => api('/api/requests/me', { token: tokenOf(getState()) }),
)

export const fetchRequest = createAsyncThunk<PurchaseRequest, string, { state: RootState }>(
  'requests/fetchOne',
  async (id, { getState }) => api(`/api/requests/${id}`, { token: tokenOf(getState()) }),
)

/**
 * Suggestions for the name being typed. Failures are swallowed rather than rejected:
 * this runs on every keystroke and a hint that could not be fetched is not something to
 * put an error banner on the page for.
 */
export const fetchSimilarRequests = createAsyncThunk<PurchaseRequest[], string>(
  'requests/fetchSimilar',
  async (itemName) => {
    try {
      return await api<PurchaseRequest[]>(
        `/api/requests/similar?itemName=${encodeURIComponent(itemName)}`,
      )
    } catch {
      return []
    }
  },
)

export const createRequest = createAsyncThunk<
  PurchaseRequest,
  CreateRequestBody,
  { state: RootState; rejectValue: CreateRejection }
>('requests/create', async (body, { getState, rejectWithValue }) => {
  try {
    return await api<PurchaseRequest>('/api/requests', {
      method: 'POST',
      body,
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue({
      message: error instanceof ApiError ? error.message : 'Could not create request',
      // A create refused because the item is already open demand names that request.
      // Keeping it means the form can offer it even when the customer submitted before
      // the suggestions arrived - the server is what enforces this, not the debounce.
      existing: existingIn(error),
    })
  }
})

export const joinRequest = createAsyncThunk<
  PurchaseRequest,
  { id: string; quantity: number },
  { state: RootState; rejectValue: string }
>('requests/join', async ({ id, quantity }, { getState, rejectWithValue }) => {
  try {
    return await api<PurchaseRequest>(`/api/requests/${id}/participants`, {
      method: 'POST',
      body: { quantity },
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not join')
  }
})

export const updateQuantity = createAsyncThunk<
  PurchaseRequest,
  { id: string; quantity: number },
  { state: RootState; rejectValue: string }
>('requests/updateQuantity', async ({ id, quantity }, { getState, rejectWithValue }) => {
  try {
    return await api<PurchaseRequest>(`/api/requests/${id}/participants/me`, {
      method: 'PUT',
      body: { quantity },
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not update')
  }
})

export const leaveRequest = createAsyncThunk<
  string,
  string,
  { state: RootState; rejectValue: string }
>('requests/leave', async (id, { getState, rejectWithValue }) => {
  try {
    await api(`/api/requests/${id}/participants/me`, {
      method: 'DELETE',
      token: tokenOf(getState()),
    })
    return id
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not leave')
  }
})

export const closeRequest = createAsyncThunk<
  PurchaseRequest,
  string,
  { state: RootState; rejectValue: string }
>('requests/close', async (id, { getState, rejectWithValue }) => {
  try {
    return await api<PurchaseRequest>(`/api/requests/${id}/close`, {
      method: 'POST',
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not close')
  }
})

const requestsSlice = createSlice({
  name: 'requests',
  initialState,
  reducers: {
    errorCleared(state) {
      state.error = null
    },
    similarCleared(state) {
      state.similar = []
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchRequests.pending, (state) => {
        state.loading = true
      })
      .addCase(fetchRequests.fulfilled, (state, action) => {
        state.loading = false
        state.browse = action.payload
      })
      .addCase(fetchMyRequests.fulfilled, (state, action) => {
        state.mine = action.payload
      })
      .addCase(fetchRequest.fulfilled, (state, action) => {
        state.current = action.payload
      })
      .addCase(fetchSimilarRequests.fulfilled, (state, action) => {
        state.similar = action.payload
      })
      .addCase(createRequest.fulfilled, (state, action) => {
        state.mine.unshift(action.payload)
        state.similar = []
        state.error = null
      })
      // Refused because the item already has an open request: show it, which is what the
      // customer needs in order to join instead. Marked exact, because that is what it
      // is and it is what the form reads to know creating is not on offer.
      .addCase(createRequest.rejected, (state, action) => {
        const existing = action.payload?.existing
        if (existing) state.similar = [{ ...existing, exact: true }]
      })
      .addCase(leaveRequest.fulfilled, (state, action) => {
        state.mine = state.mine.filter((r) => r.requestId !== action.payload)
      })
      // Joining puts the request among the caller's own. The matcher below keeps the
      // copies already held in step but only maps over them, so without this a request
      // joined from anywhere other than the My requests page stayed missing from it
      // until the next fetch - and nothing on screen could tell it had been joined.
      .addCase(joinRequest.fulfilled, (state, action) => {
        const joined = action.payload
        if (!state.mine.some((r) => r.requestId === joined.requestId)) {
          state.mine.unshift(joined)
        }
      })
      // Join, update and close all return the request in its new state, so one handler
      // keeps every copy the app is holding in step.
      .addMatcher(
        (action): action is { type: string; payload: PurchaseRequest } =>
          [joinRequest.fulfilled.type, updateQuantity.fulfilled.type, closeRequest.fulfilled.type].includes(
            action.type,
          ),
        (state, action) => {
          const updated = action.payload
          state.current = updated
          const replace = (list: PurchaseRequest[]) =>
            list.map((r) => (r.requestId === updated.requestId ? updated : r))
          state.browse = replace(state.browse)
          state.mine = replace(state.mine)
          state.error = null
        },
      )
      .addMatcher(
        (action): action is { type: string; payload?: string | CreateRejection } =>
          action.type.startsWith('requests/') && action.type.endsWith('/rejected'),
        (state, action) => {
          state.loading = false
          const payload = action.payload
          state.error = (typeof payload === 'string' ? payload : payload?.message) ?? 'Something went wrong'
        },
      )
  },
})

export const { errorCleared, similarCleared } = requestsSlice.actions
export default requestsSlice.reducer
