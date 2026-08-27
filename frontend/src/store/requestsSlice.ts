/** Purchase requests and the caller's participation in them. */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api, ApiError } from '@/api/client'
import type { CreateRequestBody, PurchaseRequest, RequestStatus } from '@/api/types'
import type { RootState } from './index'

interface RequestsState {
  browse: PurchaseRequest[]
  mine: PurchaseRequest[]
  current: PurchaseRequest | null
  loading: boolean
  error: string | null
}

const initialState: RequestsState = {
  browse: [],
  mine: [],
  current: null,
  loading: false,
  error: null,
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

export const createRequest = createAsyncThunk<
  PurchaseRequest,
  CreateRequestBody,
  { state: RootState; rejectValue: string }
>('requests/create', async (body, { getState, rejectWithValue }) => {
  try {
    return await api<PurchaseRequest>('/api/requests', {
      method: 'POST',
      body,
      token: tokenOf(getState()),
    })
  } catch (error) {
    return rejectWithValue(error instanceof ApiError ? error.message : 'Could not create request')
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
      .addCase(createRequest.fulfilled, (state, action) => {
        state.mine.unshift(action.payload)
        state.error = null
      })
      .addCase(leaveRequest.fulfilled, (state, action) => {
        state.mine = state.mine.filter((r) => r.requestId !== action.payload)
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
        (action): action is { type: string; payload?: string } =>
          action.type.startsWith('requests/') && action.type.endsWith('/rejected'),
        (state, action) => {
          state.loading = false
          state.error = action.payload ?? 'Something went wrong'
        },
      )
  },
})

export const { errorCleared } = requestsSlice.actions
export default requestsSlice.reducer
