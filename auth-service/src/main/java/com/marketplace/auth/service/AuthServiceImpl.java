package com.marketplace.auth.service;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.domain.UserStatus;
import com.marketplace.auth.dto.*;
import com.marketplace.auth.exception.*;
import com.marketplace.auth.mapper.UserMapper;
import com.marketplace.auth.domain.RefreshToken;
import com.marketplace.auth.repository.RefreshTokenRepository;
import com.marketplace.auth.repository.UserRepository;
import com.marketplace.common.security.JwtUtil;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.security.crypto.password.PasswordEncoder;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.LocalDateTime;
import java.util.HexFormat;
import java.util.UUID;

@Slf4j
@Service
@RequiredArgsConstructor
@Transactional
public class AuthServiceImpl implements AuthService {

    private final UserRepository userRepository;
    private final RefreshTokenRepository refreshTokenRepository;
    private final PasswordEncoder passwordEncoder;
    private final JwtUtil jwtUtil;
    private final UserMapper userMapper;

    @Override
    public UserResponse registerCustomer(RegisterRequest request) {
        return register(request, Role.CUSTOMER);
    }

    @Override
    public UserResponse registerSeller(RegisterRequest request) {
        return register(request, Role.SELLER);
    }

    private UserResponse register(RegisterRequest request, Role role) {
        if (userRepository.existsByEmail(request.getEmail())) {
            throw new EmailAlreadyExistsException(request.getEmail());
        }
        if (userRepository.existsByPhoneNumber(request.getPhoneNumber())) {
            throw new PhoneAlreadyExistsException(request.getPhoneNumber());
        }

        User user = User.builder()
                .email(request.getEmail())
                .phoneNumber(request.getPhoneNumber())
                .passwordHash(passwordEncoder.encode(request.getPassword()))
                .role(role)
                .status(UserStatus.ACTIVE)
                .build();

        User saved = userRepository.save(user);
        log.info("Registered new {}: userId={}, email={}", role, saved.getUserId(), saved.getEmail());
        return userMapper.toResponse(saved);
    }

    @Override
    public TokenResponse login(LoginRequest request) {
        User user = userRepository.findByEmail(request.getEmail())
                .orElseThrow(InvalidCredentialsException::new);

        if (!user.isActive()) {
            throw new UnauthorizedException("Account is blocked");
        }

        if (!passwordEncoder.matches(request.getPassword(), user.getPasswordHash())) {
            throw new InvalidCredentialsException();
        }

        String accessToken = jwtUtil.generateAccessToken(
                user.getUserId(), user.getEmail(), user.getRole().name());
        String refreshToken = jwtUtil.generateRefreshToken(user.getUserId());
        recordRefreshToken(user.getUserId(), refreshToken);

        log.info("User logged in: userId={}", user.getUserId());
        return TokenResponse.builder()
                .accessToken(accessToken)
                .refreshToken(refreshToken)
                .expiresIn(jwtUtil.getAccessTokenExpirySeconds())
                .tokenType("Bearer")
                .build();
    }

    @Override
    public TokenResponse refreshToken(String refreshToken) {
        if (!jwtUtil.isTokenValid(refreshToken) || !jwtUtil.isRefreshToken(refreshToken)) {
            throw new UnauthorizedException("Invalid refresh token");
        }

        RefreshToken stored = refreshTokenRepository.findByTokenHash(hash(refreshToken))
                .orElseThrow(() -> new UnauthorizedException("Refresh token is not recognised"));

        if (!stored.isUsable()) {
            throw new UnauthorizedException("Refresh token has been revoked or expired");
        }

        UUID userId = jwtUtil.extractUserId(refreshToken);
        User user = userRepository.findById(userId)
                .orElseThrow(() -> new UserNotFoundException("User not found"));

        if (!user.isActive()) {
            throw new UnauthorizedException("Account is blocked");
        }

        String newAccessToken = jwtUtil.generateAccessToken(
                user.getUserId(), user.getEmail(), user.getRole().name());
        String newRefreshToken = jwtUtil.generateRefreshToken(user.getUserId());

        // rotate: the presented token cannot be replayed once exchanged
        stored.setRevoked(true);
        refreshTokenRepository.save(stored);
        recordRefreshToken(user.getUserId(), newRefreshToken);

        return TokenResponse.builder()
                .accessToken(newAccessToken)
                .refreshToken(newRefreshToken)
                .expiresIn(jwtUtil.getAccessTokenExpirySeconds())
                .tokenType("Bearer")
                .build();
    }

    @Override
    public void logout(UUID userId) {
        int revoked = refreshTokenRepository.revokeAllForUser(userId);
        log.info("Logout: revoked {} refresh token(s) for userId={}", revoked, userId);
    }

    private void recordRefreshToken(UUID userId, String refreshToken) {
        refreshTokenRepository.save(RefreshToken.builder()
                .userId(userId)
                .tokenHash(hash(refreshToken))
                .expiresAt(LocalDateTime.now().plusDays(jwtUtil.getRefreshExpiryDays()))
                .revoked(false)
                .build());
    }

    /** Tokens are stored hashed so a database leak yields no usable sessions. */
    private String hash(String token) {
        try {
            return HexFormat.of().formatHex(
                    MessageDigest.getInstance("SHA-256").digest(token.getBytes(StandardCharsets.UTF_8)));
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException("SHA-256 unavailable", e);
        }
    }

    @Override
    @Transactional(readOnly = true)
    public UserResponse getCurrentUser(UUID userId) {
        User user = userRepository.findById(userId)
                .orElseThrow(() -> new UserNotFoundException("User not found: " + userId));
        return userMapper.toResponse(user);
    }

    @Override
    public UserResponse updateCurrentUser(UUID userId, UpdateUserRequest request) {
        User user = userRepository.findById(userId)
                .orElseThrow(() -> new UserNotFoundException("User not found: " + userId));

        if (request.getEmail() != null && !request.getEmail().equals(user.getEmail())) {
            if (userRepository.existsByEmail(request.getEmail())) {
                throw new EmailAlreadyExistsException(request.getEmail());
            }
            user.setEmail(request.getEmail());
        }

        if (request.getPhoneNumber() != null && !request.getPhoneNumber().equals(user.getPhoneNumber())) {
            if (userRepository.existsByPhoneNumber(request.getPhoneNumber())) {
                throw new PhoneAlreadyExistsException(request.getPhoneNumber());
            }
            user.setPhoneNumber(request.getPhoneNumber());
        }

        User updated = userRepository.save(user);
        return userMapper.toResponse(updated);
    }

    @Override
    @Transactional(readOnly = true)
    public User getUserByIdInternal(UUID userId) {
        return userRepository.findById(userId)
                .orElseThrow(() -> new UserNotFoundException("User not found: " + userId));
    }

    @Override
    @Transactional(readOnly = true)
    public PhoneResponse getPhoneByUserId(UUID userId) {
        User user = userRepository.findById(userId)
                .orElseThrow(() -> new UserNotFoundException("User not found: " + userId));
        return PhoneResponse.builder()
                .phoneNumber(user.getPhoneNumber())
                .build();
    }
}
