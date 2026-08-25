package com.marketplace.customer.client;

import com.marketplace.customer.exception.AuthServiceException;
import com.marketplace.customer.exception.InvalidUserRoleException;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClientException;
import org.springframework.web.client.RestTemplate;

import java.util.Map;
import java.util.UUID;

@Slf4j
@Component
@RequiredArgsConstructor
public class AuthClient {

    private static final String CUSTOMER_ROLE = "CUSTOMER";

    private final RestTemplate restTemplate;

    @Value("${auth.service.url}")
    private String authServiceUrl;

    /**
     * Confirms the user exists in auth-service AND is a CUSTOMER. Checking only
     * for a 2xx let a SELLER or ADMIN account create a customer profile.
     */
    public void verifyCustomer(UUID userId) {
        Map<String, Object> body;
        try {
            ResponseEntity<Map<String, Object>> response = restTemplate.exchange(
                    authServiceUrl + "/internal/users/" + userId,
                    org.springframework.http.HttpMethod.GET,
                    null,
                    new org.springframework.core.ParameterizedTypeReference<Map<String, Object>>() {});
            body = response.getBody();
        } catch (RestClientException e) {
            log.warn("Auth service call failed for userId={}: {}", userId, e.getMessage());
            throw new AuthServiceException("Auth service unavailable");
        }

        if (body == null) {
            throw new AuthServiceException("Auth service returned an empty body");
        }

        Object role = body.get("role");
        if (!CUSTOMER_ROLE.equals(role)) {
            log.warn("Refusing customer profile for userId={} with role={}", userId, role);
            throw new InvalidUserRoleException("User is not a CUSTOMER");
        }
    }
}
