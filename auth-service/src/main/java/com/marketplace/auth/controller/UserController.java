package com.marketplace.auth.controller;

import com.marketplace.auth.dto.UpdateUserRequest;
import com.marketplace.auth.dto.UserResponse;
import com.marketplace.auth.service.AuthService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.web.bind.annotation.*;

import java.util.UUID;

@RestController
@RequestMapping("/api/users")
@RequiredArgsConstructor
public class UserController {

    private final AuthService authService;

    @GetMapping("/me")
    public ResponseEntity<UserResponse> getCurrentUser(@AuthenticationPrincipal String userId) {
        return ResponseEntity.ok(authService.getCurrentUser(UUID.fromString(userId)));
    }

    @PutMapping("/me")
    public ResponseEntity<UserResponse> updateCurrentUser(
            @AuthenticationPrincipal String userId,
            @Valid @RequestBody UpdateUserRequest request) {
        return ResponseEntity.ok(authService.updateCurrentUser(UUID.fromString(userId), request));
    }
}
