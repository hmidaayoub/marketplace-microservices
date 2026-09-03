/**
 * cn is one line, and every component in the app puts its className through it. What is
 * worth testing is the twMerge half: that a caller's class overrides the component's
 * default rather than landing beside it, which is the entire reason this is not clsx.
 */

import { describe, expect, it } from 'vitest'

import { cn } from './utils'

describe('cn', () => {
  it('joins classes', () => {
    expect(cn('rounded', 'border')).toBe('rounded border')
  })

  it('lets a later class win over an earlier one in the same Tailwind group', () => {
    // Without twMerge this is "p-2 p-4" and which one applies is down to stylesheet order.
    expect(cn('p-2', 'p-4')).toBe('p-4')
  })

  it('keeps classes from groups that do not conflict', () => {
    expect(cn('px-2', 'py-4')).toBe('px-2 py-4')
  })

  it('drops falsy values, which is how the conditional call sites read', () => {
    const isHidden = false
    expect(cn('rounded', isHidden && 'hidden', null, undefined, '')).toBe('rounded')
  })

  it('takes the object and array forms clsx accepts', () => {
    expect(cn(['rounded', { border: true, hidden: false }])).toBe('rounded border')
  })

  it('is empty for no arguments rather than undefined', () => {
    expect(cn()).toBe('')
  })
})
