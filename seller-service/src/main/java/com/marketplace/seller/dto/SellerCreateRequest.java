package com.marketplace.seller.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import java.util.UUID;

public record SellerCreateRequest(
    @NotNull UUID userId,
    @NotBlank String storeName,
    String description,
    String city,
    String address
) {}
