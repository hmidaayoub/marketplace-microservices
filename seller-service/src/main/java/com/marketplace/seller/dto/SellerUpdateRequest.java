package com.marketplace.seller.dto;

public record SellerUpdateRequest(
    String storeName,
    String description,
    String city,
    String address
) {}
