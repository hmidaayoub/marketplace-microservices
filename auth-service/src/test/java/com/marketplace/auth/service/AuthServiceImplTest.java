package com.marketplace.auth.service;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.domain.UserStatus;
import com.marketplace.auth.dto.*;
import com.marketplace.auth.exception.*;
import com.marketplace.auth.mapper.UserMapper;
import com.marketplace.auth.repository.UserRepository;
import com.marketplace.common.security.JwtUtil;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.security.crypto.password.PasswordEncoder;

import java.util.Optional;
import java.util.UUID;

import static org.assertj.core.api.Assertions.*;
import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class AuthServiceImplTest {

    @Mock UserRepository userRepository;
    @Mock com.marketplace.auth.repository.RefreshTokenRepository refreshTokenRepository;
    @Mock PasswordEncoder passwordEncoder;
    @Mock JwtUtil jwtUtil;
    @Mock UserMapper userMapper;

    @InjectMocks AuthServiceImpl authService;

    private RegisterRequest registerRequest;
    private LoginRequest loginRequest;
    private User testUser;

    @BeforeEach
    void setUp() {
        registerRequest = new RegisterRequest();
        registerRequest.setEmail("test@example.com");
        registerRequest.setPassword("password123");
        registerRequest.setPhoneNumber("+1234567890");

        loginRequest = new LoginRequest();
        loginRequest.setEmail("test@example.com");
        loginRequest.setPassword("password123");

        testUser = User.builder()
                .userId(UUID.randomUUID())
                .email("test@example.com")
                .phoneNumber("+1234567890")
                .passwordHash("encoded_password")
                .role(Role.CUSTOMER)
                .status(UserStatus.ACTIVE)
                .build();
    }

    @Test
    void registerCustomer_shouldCreateUser_whenEmailAndPhoneAreUnique() {
        when(userRepository.existsByEmail(anyString())).thenReturn(false);
        when(userRepository.existsByPhoneNumber(anyString())).thenReturn(false);
        when(passwordEncoder.encode(anyString())).thenReturn("encoded_password");
        when(userRepository.save(any(User.class))).thenReturn(testUser);
        when(userMapper.toResponse(any(User.class))).thenReturn(
            UserResponse.builder().userId(testUser.getUserId()).email(testUser.getEmail()).build()
        );

        UserResponse response = authService.registerCustomer(registerRequest);

        assertThat(response).isNotNull();
        assertThat(response.getEmail()).isEqualTo("test@example.com");
        verify(userRepository).save(any(User.class));
    }

    @Test
    void registerCustomer_shouldThrow_whenEmailExists() {
        when(userRepository.existsByEmail(anyString())).thenReturn(true);

        assertThatThrownBy(() -> authService.registerCustomer(registerRequest))
                .isInstanceOf(EmailAlreadyExistsException.class);
    }

    @Test
    void registerSeller_shouldCreateUser_withSellerRole() {
        when(userRepository.existsByEmail(anyString())).thenReturn(false);
        when(userRepository.existsByPhoneNumber(anyString())).thenReturn(false);
        when(passwordEncoder.encode(anyString())).thenReturn("encoded_password");
        when(userRepository.save(any(User.class))).thenReturn(testUser);
        when(userMapper.toResponse(any(User.class))).thenReturn(
            UserResponse.builder().userId(testUser.getUserId()).email(testUser.getEmail()).build()
        );

        UserResponse response = authService.registerSeller(registerRequest);
        assertThat(response).isNotNull();
    }

    @Test
    void login_shouldReturnTokens_whenCredentialsValid() {
        when(userRepository.findByEmail(anyString())).thenReturn(Optional.of(testUser));
        when(passwordEncoder.matches(anyString(), anyString())).thenReturn(true);
        when(jwtUtil.generateAccessToken(any(), anyString(), anyString())).thenReturn("access_token");
        when(jwtUtil.generateRefreshToken(any())).thenReturn("refresh_token");
        when(jwtUtil.getAccessTokenExpirySeconds()).thenReturn(3600L);

        TokenResponse response = authService.login(loginRequest);

        assertThat(response).isNotNull();
        assertThat(response.getAccessToken()).isEqualTo("access_token");
        assertThat(response.getRefreshToken()).isEqualTo("refresh_token");
        assertThat(response.getExpiresIn()).isEqualTo(3600);
    }

    @Test
    void login_shouldThrow_whenUserNotFound() {
        when(userRepository.findByEmail(anyString())).thenReturn(Optional.empty());

        assertThatThrownBy(() -> authService.login(loginRequest))
                .isInstanceOf(InvalidCredentialsException.class);
    }

    @Test
    void login_shouldThrow_whenPasswordMismatch() {
        when(userRepository.findByEmail(anyString())).thenReturn(Optional.of(testUser));
        when(passwordEncoder.matches(anyString(), anyString())).thenReturn(false);

        assertThatThrownBy(() -> authService.login(loginRequest))
                .isInstanceOf(InvalidCredentialsException.class);
    }

    @Test
    void login_shouldThrow_whenUserBlocked() {
        testUser.setStatus(UserStatus.BLOCKED);
        when(userRepository.findByEmail(anyString())).thenReturn(Optional.of(testUser));

        assertThatThrownBy(() -> authService.login(loginRequest))
                .isInstanceOf(UnauthorizedException.class)
                .hasMessageContaining("blocked");
    }

    @Test
    void getPhoneByUserId_shouldReturnPhone_whenUserExists() {
        when(userRepository.findById(any())).thenReturn(Optional.of(testUser));

        PhoneResponse response = authService.getPhoneByUserId(testUser.getUserId());

        assertThat(response.getPhoneNumber()).isEqualTo("+1234567890");
    }

    @Test
    void getPhoneByUserId_shouldThrow_whenUserNotFound() {
        when(userRepository.findById(any())).thenReturn(Optional.empty());

        assertThatThrownBy(() -> authService.getPhoneByUserId(UUID.randomUUID()))
                .isInstanceOf(UserNotFoundException.class);
    }
}
