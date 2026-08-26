package com.marketplace.auth.service;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.dto.*;

import java.util.List;
import java.util.UUID;

public interface AuthService {
    UserResponse registerCustomer(RegisterRequest request);
    UserResponse registerSeller(RegisterRequest request);
    TokenResponse login(LoginRequest request);
    TokenResponse refreshToken(String refreshToken);
    void logout(UUID userId);
    UserResponse getCurrentUser(UUID userId);
    UserResponse updateCurrentUser(UUID userId, UpdateUserRequest request);
    User getUserByIdInternal(UUID userId);
    PhoneResponse getPhoneByUserId(UUID userId);
    List<User> getActiveUsersByRole(Role role);
}
