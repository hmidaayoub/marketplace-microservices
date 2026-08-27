import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'

import AppLayout from '@/components/app-layout'
import ProtectedRoute from '@/components/protected-route'
import BrowseRequests from '@/pages/BrowseRequests'
import Contacts from '@/pages/Contacts'
import Login from '@/pages/Login'
import MyOffers from '@/pages/MyOffers'
import MyRequests from '@/pages/MyRequests'
import Notifications from '@/pages/Notifications'
import ProfileSetup from '@/pages/ProfileSetup'
import Register from '@/pages/Register'
import RequestDetail from '@/pages/RequestDetail'
import Access from '@/pages/admin/Access'
import Queue from '@/pages/admin/Queue'
import { useAppDispatch, useAppSelector } from '@/store'
import { loadSession } from '@/store/authSlice'

export default function App() {
  const dispatch = useAppDispatch()
  const { accessToken, user } = useAppSelector((s) => s.auth)

  // A token restored from storage says nothing about who it belongs to or whether the
  // role profile exists, and both gate routing - so the session is loaded before the
  // guarded tree renders anything.
  useEffect(() => {
    if (accessToken && !user) dispatch(loadSession())
  }, [accessToken, dispatch, user])

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/register" element={<Register />} />

      <Route
        path="/profile"
        element={
          <ProtectedRoute>
            <ProfileSetup />
          </ProtectedRoute>
        }
      />

      {/* The shell itself is not gated. Browsing demand is the one thing a visitor may
          do before signing up - everything under it that writes, or that is somebody's
          own data, still carries its own guard. */}
      <Route element={<AppLayout />}>
        <Route path="/requests" element={<BrowseRequests />} />
        <Route path="/requests/:id" element={<RequestDetail />} />

        <Route
          path="/notifications"
          element={
            <ProtectedRoute>
              <Notifications />
            </ProtectedRoute>
          }
        />

        <Route
          path="/my-requests"
          element={
            <ProtectedRoute roles={['CUSTOMER']}>
              <MyRequests />
            </ProtectedRoute>
          }
        />
        <Route
          path="/my-offers"
          element={
            <ProtectedRoute roles={['SELLER']}>
              <MyOffers />
            </ProtectedRoute>
          }
        />
        <Route
          path="/contacts"
          element={
            <ProtectedRoute roles={['SELLER']}>
              <Contacts />
            </ProtectedRoute>
          }
        />
        <Route
          path="/admin/queue"
          element={
            <ProtectedRoute roles={['ADMIN']}>
              <Queue />
            </ProtectedRoute>
          }
        />
        <Route
          path="/admin/access"
          element={
            <ProtectedRoute roles={['ADMIN']}>
              <Access />
            </ProtectedRoute>
          }
        />
      </Route>

      <Route path="*" element={<Navigate to="/requests" replace />} />
    </Routes>
  )
}
