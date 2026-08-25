package com.marketplace.common.security;

import io.jsonwebtoken.*;
import io.jsonwebtoken.security.Keys;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import javax.crypto.SecretKey;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.Date;
import java.util.UUID;

@Slf4j
@Component
public class JwtUtil {

    private final SecretKey secretKey;
    private final long expiryMinutes;
    private final long refreshExpiryDays;

    public JwtUtil(
            @Value("${jwt.secret}") String secret,
            @Value("${jwt.expiry-minutes:60}") long expiryMinutes,
            @Value("${jwt.refresh-expiry-days:7}") long refreshExpiryDays) {
        this.secretKey = Keys.hmacShaKeyFor(secret.getBytes(StandardCharsets.UTF_8));
        this.expiryMinutes = expiryMinutes;
        this.refreshExpiryDays = refreshExpiryDays;
    }

    /** Access-token lifetime in seconds, for the expiresIn field of a token response. */
    public long getAccessTokenExpirySeconds() {
        return expiryMinutes * 60;
    }

    public String generateAccessToken(UUID userId, String email, String role) {
        Instant now = Instant.now();
        Instant expiry = now.plus(expiryMinutes, ChronoUnit.MINUTES);

        return Jwts.builder()
                .subject(userId.toString())
                .claim("email", email)
                .claim("role", role)
                .issuedAt(Date.from(now))
                .expiration(Date.from(expiry))
                .signWith(secretKey)
                .compact();
    }

    public String generateRefreshToken(UUID userId) {
        Instant now = Instant.now();
        Instant expiry = now.plus(refreshExpiryDays, ChronoUnit.DAYS);

        return Jwts.builder()
                .subject(userId.toString())
                .claim("type", "refresh")
                .issuedAt(Date.from(now))
                .expiration(Date.from(expiry))
                .signWith(secretKey)
                .compact();
    }

    public Claims parseToken(String token) {
        try {
            return Jwts.parser()
                    .verifyWith(secretKey)
                    .build()
                    .parseSignedClaims(token)
                    .getPayload();
        } catch (ExpiredJwtException e) {
            log.warn("JWT token expired");
            throw new RuntimeException("Token expired");
        } catch (JwtException e) {
            log.warn("Invalid JWT token: {}", e.getMessage());
            throw new RuntimeException("Invalid token");
        }
    }

    public boolean isTokenValid(String token) {
        try {
            parseToken(token);
            return true;
        } catch (Exception e) {
            return false;
        }
    }

    public UUID extractUserId(String token) {
        Claims claims = parseToken(token);
        return UUID.fromString(claims.getSubject());
    }

    public String extractRole(String token) {
        Claims claims = parseToken(token);
        return claims.get("role", String.class);
    }

    public boolean isRefreshToken(String token) {
        Claims claims = parseToken(token);
        return "refresh".equals(claims.get("type", String.class));
    }
}
