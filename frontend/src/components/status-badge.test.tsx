/**
 * The badge is a lookup table, so the tests are about the two things a table gets wrong:
 * a key that is not in it, and a label that does not say what the status means.
 */

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { StatusBadge } from './status-badge'

describe('StatusBadge', () => {
  it.each([
    ['OPEN', 'Open'],
    ['PENDING', 'Pending review'],
    ['APPROVED', 'Approved'],
    ['REJECTED', 'Rejected'],
    ['GRANTED', 'Granted'],
    ['REVOKED', 'Revoked'],
    ['EXPIRED', 'Expired'],
  ])('labels %s as "%s"', (status, label) => {
    render(<StatusBadge status={status} />)

    expect(screen.getByText(label)).toBeInTheDocument()
  })

  it('calls INACTIVE "Dormant", because a request with no buyers is not a fault', () => {
    render(<StatusBadge status="INACTIVE" />)

    expect(screen.getByText('Dormant')).toBeInTheDocument()
    // "No buyers" would be wrong: a request held open by a seller's offer has none either.
    expect(screen.queryByText(/no buyers/i)).not.toBeInTheDocument()
  })

  it('keeps the raw status in a title, so the wire value is recoverable from the UI', () => {
    render(<StatusBadge status="APPROVED" />)

    expect(screen.getByTitle('APPROVED')).toBeInTheDocument()
  })

  it('shows an unrecognised status verbatim rather than rendering nothing', () => {
    render(<StatusBadge status="SOMETHING_NEW" />)

    expect(screen.getByText('SOMETHING_NEW')).toBeInTheDocument()
  })

  it('renders UNKNOWN when the status is missing entirely', () => {
    render(<StatusBadge status={null} />)

    expect(screen.getByText('UNKNOWN')).toBeInTheDocument()
  })

  it('passes the caller className through alongside the status colour', () => {
    const { container } = render(<StatusBadge status="OPEN" className="ml-2" />)

    expect(container.firstElementChild).toHaveClass('ml-2')
    expect(container.firstElementChild).toHaveClass('text-success')
  })
})
