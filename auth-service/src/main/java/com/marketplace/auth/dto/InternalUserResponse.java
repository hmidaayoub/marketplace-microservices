package com.marketplace.auth.dto;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.UserStatus;
import lombok.Builder;
import lombok.Data;

import java.util.UUID;

/**
 * Identity data exposed to other backend services via GET /internal/users/{userId}.
 * Deliberately excludes phoneNumber: per spec section 7 the phone number is only
 * available through the restricted GET /internal/users/{userId}/phone endpoint.
 */
@Data
@Builder
public class InternalUserResponse {
    private UUID userId;
    private String email;
    private Role role;
    private UserStatus status;
}
