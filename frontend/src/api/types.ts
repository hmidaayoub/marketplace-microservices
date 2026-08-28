/**
 * Names for the shapes the app passes around.
 *
 * Everything here is pulled from the generated schemas rather than written out, so a
 * backend change that alters a response surfaces as a type error instead of a runtime
 * surprise. Regenerate with `npm run codegen`.
 */

import type { components as AdminSchema } from './schema/admin'
import type { components as AuthSchema } from './schema/auth'
import type { components as CustomerSchema } from './schema/customer'
import type { components as NotificationSchema } from './schema/notification'
import type { components as OfferSchema } from './schema/offer'
import type { components as RequestSchema } from './schema/request'
import type { components as SellerSchema } from './schema/seller'

export type Role = 'CUSTOMER' | 'SELLER' | 'ADMIN'

export type AuthTokens = AuthSchema['schemas']['TokenResponse']
export type User = AuthSchema['schemas']['UserResponse']
export type Customer = CustomerSchema['schemas']['CustomerResponse']
export type Seller = SellerSchema['schemas']['SellerResponse']

export type PurchaseRequest = RequestSchema['schemas']['requests.requestResponse']
export type CreateRequestBody = RequestSchema['schemas']['requests.createRequestBody']

export type Offer = OfferSchema['schemas']['OfferOut']
/** What a rival seller sees instead: same endpoint, sellerId withheld. */
export type CompetingOffer = OfferSchema['schemas']['CompetingOfferOut']
export type AnyOffer = Offer | CompetingOffer
export type OfferCreate = OfferSchema['schemas']['OfferCreate']

export type PendingOffer = AdminSchema['schemas']['admin.pendingOfferResponse']
export type Decision = AdminSchema['schemas']['admin.decisionResponse']
export type ContactList = AdminSchema['schemas']['admin.contactsResponse']
export type ContactAccess = AdminSchema['schemas']['admin.contactAccessResponse']

export type Notification = NotificationSchema['schemas']['NotificationOut']
export type UnreadCount = NotificationSchema['schemas']['UnreadCountOut']

export type RequestStatus = 'OPEN' | 'INACTIVE'

/**
 * The rival-seller projection withholds sellerId, so its presence is what distinguishes
 * the two shapes - a discriminator the backend gives us for free.
 */
export function isFullOffer(offer: AnyOffer): offer is Offer {
  return 'sellerId' in offer && offer.sellerId != null
}
