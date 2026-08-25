package com.marketplace.auth.controller;

import com.marketplace.auth.domain.User;
import com.marketplace.auth.dto.InternalUserResponse;
import com.marketplace.auth.dto.PhoneResponse;
import com.marketplace.auth.mapper.UserMapper;
import com.marketplace.auth.service.AuthService;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

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
}
