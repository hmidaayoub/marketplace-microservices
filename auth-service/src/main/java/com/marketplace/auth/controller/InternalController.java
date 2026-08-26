package com.marketplace.auth.controller;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.dto.InternalUserResponse;
import com.marketplace.auth.dto.PhoneResponse;
import com.marketplace.auth.mapper.UserMapper;
import com.marketplace.auth.service.AuthService;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.UUID;

@RestController
@RequestMapping("/internal")
@RequiredArgsConstructor
public class InternalController {

    private final AuthService authService;
    private final UserMapper userMapper;

    @GetMapping("/users/{userId}")
    public ResponseEntity<InternalUserResponse> getUserById(@PathVariable UUID userId) {
        User user = authService.getUserByIdInternal(userId);
        return ResponseEntity.ok(userMapper.toInternalResponse(user));
    }

    @GetMapping("/users/{userId}/phone")
    public ResponseEntity<PhoneResponse> getPhoneByUserId(@PathVariable UUID userId) {
        return ResponseEntity.ok(authService.getPhoneByUserId(userId));
    }

    /**
     * Every ACTIVE user holding a role, as {@link InternalUserResponse} - so still no
     * phone number.
     *
     * Spec section 18 addresses NEW_OFFER to "Admin" rather than to one person, and
     * roles are owned here. Notification-service is addressed by userId and never
     * resolves an identity, so offer-service asks this endpoint who the admins are and
     * puts the resulting userIds on the event.
     *
     * A role is a path variable rather than a free query parameter so an unknown value
     * is a 400 from Spring's own conversion, identical to a malformed UUID elsewhere.
     */
    @GetMapping("/users/by-role/{role}")
    public ResponseEntity<List<InternalUserResponse>> getUsersByRole(@PathVariable Role role) {
        List<InternalUserResponse> users = authService.getActiveUsersByRole(role).stream()
                .map(userMapper::toInternalResponse)
                .toList();
        return ResponseEntity.ok(users);
    }
}
