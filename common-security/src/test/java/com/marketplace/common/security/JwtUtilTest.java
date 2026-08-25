package com.marketplace.common.security;

import org.junit.jupiter.api.Test;

import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class JwtUtilTest {

    private static final String SECRET = "test-secret-key-for-unit-tests-only-256-bits-long";

    private final JwtUtil jwtUtil = new JwtUtil(SECRET, 15, 7);

    @Test
    void parseToken_shouldThrowInvalidTokenException_whenTokenIsGarbage() {
        assertThatThrownBy(() -> jwtUtil.parseToken("not-a-jwt"))
                .isInstanceOf(InvalidTokenException.class);
    }

    @Test
    void parseToken_shouldThrowInvalidTokenException_whenSignatureIsWrong() {
        String foreign = new JwtUtil("a-completely-different-signing-key-of-sufficient-length", 15, 7)
                .generateAccessToken(UUID.randomUUID(), "a@b.com", "CUSTOMER");

        assertThatThrownBy(() -> jwtUtil.parseToken(foreign))
                .isInstanceOf(InvalidTokenException.class);
    }

    @Test
    void roundTrip_shouldPreserveSubjectAndRole() {
        UUID userId = UUID.randomUUID();
        String token = jwtUtil.generateAccessToken(userId, "a@b.com", "SELLER");

        assertThat(jwtUtil.extractUserId(token)).isEqualTo(userId);
        assertThat(jwtUtil.extractRole(token)).isEqualTo("SELLER");
        assertThat(jwtUtil.isTokenValid(token)).isTrue();
    }

    @Test
    void getAccessTokenExpirySeconds_shouldReflectConfiguredMinutes() {
        assertThat(jwtUtil.getAccessTokenExpirySeconds()).isEqualTo(900L);
    }

    @Test
    void isRefreshToken_shouldDistinguishTokenTypes() {
        UUID userId = UUID.randomUUID();
        assertThat(jwtUtil.isRefreshToken(jwtUtil.generateRefreshToken(userId))).isTrue();
        assertThat(jwtUtil.isRefreshToken(
                jwtUtil.generateAccessToken(userId, "a@b.com", "CUSTOMER"))).isFalse();
    }

    @Test
    void generateRefreshToken_shouldBeUnique_forRepeatedCallsInTheSameSecond() {
        UUID userId = UUID.randomUUID();
        assertThat(jwtUtil.generateRefreshToken(userId))
                .isNotEqualTo(jwtUtil.generateRefreshToken(userId));
    }

}
