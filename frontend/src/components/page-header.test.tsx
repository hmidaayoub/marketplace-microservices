/**
 * A layout component, so the tests are about what it omits: a description block for a
 * page with no description, and an action row for a page with no action, either of
 * which would otherwise leave a gap in the flex layout.
 */

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PageHeader } from './page-header'

describe('PageHeader', () => {
  it('renders the title as the page heading, not as styled text', () => {
    render(<PageHeader title="My requests" />)

    expect(screen.getByRole('heading', { level: 1, name: 'My requests' })).toBeInTheDocument()
  })

  it('shows the description when there is one', () => {
    render(<PageHeader title="My requests" description="Everything you have asked for." />)

    expect(screen.getByText('Everything you have asked for.')).toBeInTheDocument()
  })

  it('renders no description paragraph when there is none', () => {
    const { container } = render(<PageHeader title="My requests" />)

    expect(container.querySelector('p')).toBeNull()
  })

  it('renders the action a page leads with', () => {
    render(
      <PageHeader title="My requests">
        <button type="button">New request</button>
      </PageHeader>,
    )

    expect(screen.getByRole('button', { name: 'New request' })).toBeInTheDocument()
  })

  it('takes a node for the description, not only a string', () => {
    render(<PageHeader title="Offers" description={<strong>Three pending</strong>} />)

    expect(screen.getByText('Three pending').tagName).toBe('STRONG')
  })
})
