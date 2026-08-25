package com.marketplace.common.security;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.MediaType;
import org.springframework.security.authentication.UsernamePasswordAuthenticationToken;
import org.springframework.security.core.authority.SimpleGrantedAuthority;
import org.springframework.security.core.context.SecurityContextHolder;
import org.springframework.stereotype.Component;
import org.springframework.util.StringUtils;
import org.springframework.web.filter.OncePerRequestFilter;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Collections;

/**
 * Guards /internal/** so only authorized backend services can reach it (spec section 6).
 * Callers present a shared secret in X-Internal-Api-Key; a match grants the INTERNAL
 * authority that SecurityConfig requires on these routes. Anything else gets 401 —
 * including the case where no key is configured, so a misconfigured deployment fails
 * closed rather than serving phone numbers to the world.
 */
@Slf4j
@Component
public class InternalApiKeyFilter extends OncePerRequestFilter {

    public static final String INTERNAL_API_KEY_HEADER = "X-Internal-Api-Key";
    public static final String INTERNAL_AUTHORITY = "INTERNAL";

    private static final String INTERNAL_PATH_PREFIX = "/internal/";

    private final String configuredKey;

    public InternalApiKeyFilter(@Value("${internal.api.key:}") String configuredKey) {
        this.configuredKey = configuredKey;
    }

    @Override
    protected boolean shouldNotFilter(HttpServletRequest request) {
        return !request.getRequestURI().startsWith(INTERNAL_PATH_PREFIX);
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request,
                                    HttpServletResponse response,
                                    FilterChain filterChain) throws ServletException, IOException {
        if (!StringUtils.hasText(configuredKey)) {
            log.error("internal.api.key is not configured - rejecting all /internal requests");
            reject(response);
            return;
        }

        if (!matchesConfiguredKey(request.getHeader(INTERNAL_API_KEY_HEADER))) {
            log.warn("Rejected /internal request to {}: missing or invalid API key", request.getRequestURI());
            reject(response);
            return;
        }

        UsernamePasswordAuthenticationToken authentication = new UsernamePasswordAuthenticationToken(
                INTERNAL_AUTHORITY, null,
                Collections.singletonList(new SimpleGrantedAuthority(INTERNAL_AUTHORITY)));
        SecurityContextHolder.getContext().setAuthentication(authentication);

        filterChain.doFilter(request, response);
    }

    /** Constant-time comparison: a plain equals() leaks the key through response timing. */
    private boolean matchesConfiguredKey(String presentedKey) {
        if (!StringUtils.hasText(presentedKey)) {
            return false;
        }
        return MessageDigest.isEqual(
                presentedKey.getBytes(StandardCharsets.UTF_8),
                configuredKey.getBytes(StandardCharsets.UTF_8));
    }

    private void reject(HttpServletResponse response) throws IOException {
        SecurityContextHolder.clearContext();
        response.setStatus(HttpServletResponse.SC_UNAUTHORIZED);
        response.setContentType(MediaType.APPLICATION_JSON_VALUE);
        response.getWriter().write("{\"message\":\"Unauthorized internal request\",\"status\":401}");
    }
}
