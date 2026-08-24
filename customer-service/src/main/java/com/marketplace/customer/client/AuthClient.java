package com.marketplace.customer.client;

import com.marketplace.customer.exception.AuthServiceException;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestClientException;
import org.springframework.web.client.RestTemplate;

import java.util.UUID;

@Slf4j
@Component
@RequiredArgsConstructor
public class AuthClient {

    private final RestTemplate restTemplate;

    @Value("${auth.service.url}")
    private String authServiceUrl;

    public boolean userExists(UUID userId) {
        try {
            ResponseEntity<Void> response = restTemplate.getForEntity(
                    authServiceUrl + "/internal/users/" + userId,
                    Void.class);
            return response.getStatusCode().is2xxSuccessful();
        } catch (RestClientException e) {
            log.warn("Auth service call failed for userId={}: {}", userId, e.getMessage());
            throw new AuthServiceException("Auth service unavailable");
        }
    }
}
