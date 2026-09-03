/**
 * The component exists so nine call sites can pass a nullable error straight in, which
 * makes "renders nothing when there is no error" the behaviour it is actually for.
 */

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ErrorAlert } from './error-alert'

describe('ErrorAlert', () => {
  it('renders nothing at all when there is no error', () => {
    const { container } = render(<ErrorAlert>{null}</ErrorAlert>)

    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for a false, which is how the conditional call sites read', () => {
    // `as const` because JSX widens a literal to boolean, and the prop takes `false`
    // specifically - the shape `error && error.message` produces at the call sites.
    const { container } = render(<ErrorAlert>{false as const}</ErrorAlert>)

    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for an empty string rather than an empty box', () => {
    const { container } = render(<ErrorAlert>{''}</ErrorAlert>)

    expect(container).toBeEmptyDOMElement()
  })

  it('shows the message with the default error title', () => {
    render(<ErrorAlert>Email already registered</ErrorAlert>)

    expect(screen.getByText('Email already registered')).toBeInTheDocument()
    expect(screen.getByText('That did not work')).toBeInTheDocument()
  })

  it('takes a caller title over the default', () => {
    render(<ErrorAlert title="Could not save">Server unavailable</ErrorAlert>)

    expect(screen.getByText('Could not save')).toBeInTheDocument()
    expect(screen.queryByText('That did not work')).not.toBeInTheDocument()
  })

  it('titles the info tone differently, because it is not reporting a failure', () => {
    render(<ErrorAlert tone="info">Approval can take a day</ErrorAlert>)

    expect(screen.getByText('Heads up')).toBeInTheDocument()
  })

  it('is an alert, so a screen reader announces it without being asked', () => {
    render(<ErrorAlert>Something went wrong</ErrorAlert>)

    expect(screen.getByRole('alert')).toBeInTheDocument()
  })
})
