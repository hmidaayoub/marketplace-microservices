package com.marketplace.auth.service;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.dto.*;

import java.util.UUID;

public interface AuthService {
    UserResponse registerCustomer(RegisterRequest request);
    UserResponse registerSeller(RegisterRequest request);
    TokenResponse login(LoginRequest request);
    TokenResponse refreshToken(String refreshToken);
    void logout(String token);
    UserResponse getCurrentUser(UUID userId);
    UserResponse updateCurrentUser(UUID userId, UpdateUserRequest request);
    User getUserByIdInternal(UUID userId);
    PhoneResponse getPhoneByUserId(UUID userId);
}
