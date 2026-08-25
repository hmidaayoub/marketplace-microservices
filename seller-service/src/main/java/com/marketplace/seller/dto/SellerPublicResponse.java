package com.marketplace.seller.dto;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Seller profile as shown to anyone, per spec section 9.
 * Excludes address (the seller's physical location) and userId (their
 * identity in the auth service); both are visible only to the owner.
 */
public record SellerPublicResponse(
    UUID sellerId,
    String storeName,
    String description,
    String city,
    Double rating,
    LocalDateTime createdAt
) {}
