/**
 * isFullOffer is the app's only discriminator between the two offer projections, and
 * both branches route on it: the seller who owns an offer may edit it, a rival seeing
 * the same endpoint's redacted shape may not. Getting it wrong shows a rival an edit
 * button, so the interesting cases are the ones where sellerId is present but empty.
 */

import { describe, expect, it } from 'vitest'

import { isFullOffer, type AnyOffer } from './types'

describe('isFullOffer', () => {
  it('is true for the owner projection, which carries sellerId', () => {
    expect(isFullOffer({ id: 1, sellerId: 4 } as unknown as AnyOffer)).toBe(true)
  })

  it('is false for the rival projection, which withholds the key entirely', () => {
    expect(isFullOffer({ id: 1, price: 30 } as unknown as AnyOffer)).toBe(false)
  })

  it('is false when the key is present but null - absent and redacted read the same', () => {
    expect(isFullOffer({ id: 1, sellerId: null } as unknown as AnyOffer)).toBe(false)
  })

  it('is false when the key is present but undefined', () => {
    expect(isFullOffer({ id: 1, sellerId: undefined } as unknown as AnyOffer)).toBe(false)
  })

  it('is true for seller id 0, which is a value and not an absence', () => {
    expect(isFullOffer({ id: 1, sellerId: 0 } as unknown as AnyOffer)).toBe(true)
  })
})
