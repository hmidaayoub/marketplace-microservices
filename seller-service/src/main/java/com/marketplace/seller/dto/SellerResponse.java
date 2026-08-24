package com.marketplace.seller.dto;

import java.time.LocalDateTime;
import java.util.UUID;

public record SellerResponse(
    UUID sellerId,
    UUID userId,
    String storeName,
    String description,
    String city,
    String address,
    Double rating,
    LocalDateTime createdAt,
    LocalDateTime updatedAt
) {}
