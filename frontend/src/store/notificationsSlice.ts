/**
 * The user's notification inbox.
 *
 * Notifications are eventually consistent by design: a producer writes the event to its
 * outbox inside the business transaction, and a relay publishes it to RabbitMQ on a
 * two-second tick. So the notification for an action does not exist yet when the action
 * returns, and refetching immediately is a race the UI would lose. The inbox polls
 * instead - see startPolling in the layout.
 */

import { createAsyncThunk, createSlice } from '@reduxjs/toolkit'

import { api } from '../api/client'
import type { Notification, UnreadCount } from '../api/types'
import type { RootState } from './index'

interface NotificationsState {
  items: Notification[]
  unread: number
  loading: boolean
}

const initialState: NotificationsState = { items: [], unread: 0, loading: false }

const tokenOf = (state: RootState) => state.auth.accessToken

export const fetchNotifications = createAsyncThunk<Notification[], void, { state: RootState }>(
  'notifications/fetch',
  async (_, { getState }) => api('/api/notifications/me', { token: tokenOf(getState()) }),
)

export const fetchUnreadCount = createAsyncThunk<UnreadCount, void, { state: RootState }>(
  'notifications/unread',
  async (_, { getState }) => api('/api/notifications/me/unread-count', { token: tokenOf(getState()) }),
)

export const markRead = createAsyncThunk<Notification, string, { state: RootState }>(
  'notifications/markRead',
  async (id, { getState }) =>
    api(`/api/notifications/${id}/read`, { method: 'PATCH', token: tokenOf(getState()) }),
)

const notificationsSlice = createSlice({
  name: 'notifications',
  initialState,
  reducers: {},
  extraReducers: (builder) => {
    builder
      .addCase(fetchNotifications.pending, (state) => {
        state.loading = true
      })
      .addCase(fetchNotifications.fulfilled, (state, action) => {
        state.loading = false
        state.items = action.payload
      })
      .addCase(fetchUnreadCount.fulfilled, (state, action) => {
        state.unread = action.payload.unreadCount ?? 0
      })
      .addCase(markRead.fulfilled, (state, action) => {
        state.items = state.items.map((n) =>
          n.notificationId === action.payload.notificationId ? action.payload : n,
        )
        state.unread = Math.max(0, state.unread - 1)
      })
  },
})

export default notificationsSlice.reducer
