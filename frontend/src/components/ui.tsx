/** The handful of Tailwind primitives the pages share. */

import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, SelectHTMLAttributes } from 'react'

export function Button({
  variant = 'primary',
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'ghost' | 'danger' }) {
  const styles = {
    primary: 'bg-brand-600 text-white hover:bg-brand-700 disabled:bg-slate-300',
    ghost: 'bg-white text-slate-700 ring-1 ring-slate-300 hover:bg-slate-50',
    danger: 'bg-red-600 text-white hover:bg-red-700',
  }[variant]
  return (
    <button
      className={`rounded-md px-3 py-2 text-sm font-medium transition disabled:cursor-not-allowed ${styles} ${className}`}
      {...props}
    />
  )
}

export function Input({ className = '', ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`w-full rounded-md border border-slate-300 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100 ${className}`}
      {...props}
    />
  )
}

export function Select({ className = '', ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-2 focus:ring-brand-100 ${className}`}
      {...props}
    />
  )
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm font-medium text-slate-700">{label}</span>
      {children}
    </label>
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`rounded-lg border border-slate-200 bg-white p-4 shadow-sm ${className}`}>
      {children}
    </div>
  )
}

const STATUS_STYLES: Record<string, string> = {
  OPEN: 'bg-emerald-100 text-emerald-800',
  OFFER_APPROVED: 'bg-brand-100 text-brand-700',
  CLOSED: 'bg-slate-200 text-slate-600',
  PENDING: 'bg-amber-100 text-amber-800',
  APPROVED: 'bg-emerald-100 text-emerald-800',
  REJECTED: 'bg-red-100 text-red-700',
}

export function Badge({ children }: { children: string }) {
  const style = STATUS_STYLES[children] ?? 'bg-slate-100 text-slate-700'
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${style}`}>{children}</span>
  )
}

export function Alert({ children, tone = 'error' }: { children: ReactNode; tone?: 'error' | 'info' }) {
  if (!children) return null
  const style =
    tone === 'error'
      ? 'border-red-200 bg-red-50 text-red-800'
      : 'border-brand-100 bg-brand-50 text-brand-700'
  return <div className={`rounded-md border px-3 py-2 text-sm ${style}`}>{children}</div>
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="py-8 text-center text-sm text-slate-500">{children}</p>
}
