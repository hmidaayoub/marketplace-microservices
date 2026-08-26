import { configureStore } from '@reduxjs/toolkit'
import { useDispatch, useSelector } from 'react-redux'

import adminReducer from './adminSlice'
import authReducer from './authSlice'
import notificationsReducer from './notificationsSlice'
import offersReducer from './offersSlice'
import requestsReducer from './requestsSlice'

export const store = configureStore({
  reducer: {
    auth: authReducer,
    requests: requestsReducer,
    offers: offersReducer,
    admin: adminReducer,
    notifications: notificationsReducer,
  },
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch

export const useAppDispatch = useDispatch.withTypes<AppDispatch>()
export const useAppSelector = useSelector.withTypes<RootState>()
