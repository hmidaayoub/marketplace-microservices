package com.marketplace.seller.dto;

import jakarta.validation.constraints.NotBlank;

/**
 * userId is deliberately absent: it comes from the authenticated JWT subject.
 * Accepting it in the body let any caller create a profile for another user.
 */
public record SellerCreateRequest(
    @NotBlank String storeName,
    String description,
    String city,
    String address
) {}
