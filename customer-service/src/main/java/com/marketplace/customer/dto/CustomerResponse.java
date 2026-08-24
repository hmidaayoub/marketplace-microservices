package com.marketplace.customer.dto;

import com.marketplace.customer.domain.ProfileStatus;
import lombok.Builder;
import lombok.Data;

import java.time.LocalDateTime;
import java.util.UUID;

@Data
@Builder
public class CustomerResponse {
    private UUID customerId;
    private UUID userId;
    private String firstName;
    private String lastName;
    private ProfileStatus profileStatus;
    private LocalDateTime createdAt;
    private LocalDateTime updatedAt;
}
