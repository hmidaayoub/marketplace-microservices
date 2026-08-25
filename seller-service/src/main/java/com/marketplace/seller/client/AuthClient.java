package com.marketplace.seller.client;

import com.marketplace.seller.exception.AuthServiceException;
import com.marketplace.seller.exception.InvalidUserRoleException;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.core.ParameterizedTypeReference;
import org.springframework.http.HttpMethod;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClientException;
import org.springframework.web.client.RestTemplate;

import java.util.Map;
import java.util.UUID;

/**
 * Confirms with auth-service that the caller exists and holds the SELLER role
 * before a store profile is created (spec section 15, steps 3-4).
 */
@Slf4j
@Component
@RequiredArgsConstructor
public class AuthClient {

    private static final String SELLER_ROLE = "SELLER";

    private final RestTemplate restTemplate;

    @Value("${auth.service.url}")
    private String authServiceUrl;

    public void verifySeller(UUID userId) {
        Map<String, Object> body;
        try {
            ResponseEntity<Map<String, Object>> response = restTemplate.exchange(
                    authServiceUrl + "/internal/users/" + userId,
                    HttpMethod.GET,
                    null,
                    new ParameterizedTypeReference<Map<String, Object>>() {});
            body = response.getBody();
        } catch (RestClientException e) {
            log.warn("Auth service call failed for userId={}: {}", userId, e.getMessage());
            throw new AuthServiceException("Auth service unavailable");
        }

        if (body == null) {
            throw new AuthServiceException("Auth service returned an empty body");
        }

        Object role = body.get("role");
        if (!SELLER_ROLE.equals(role)) {
            log.warn("Refusing seller profile for userId={} with role={}", userId, role);
            throw new InvalidUserRoleException("User is not a SELLER");
        }
    }
}
