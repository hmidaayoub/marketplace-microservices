package com.marketplace.auth.config;

import com.marketplace.common.security.InternalApiKeyFilter;
import com.marketplace.common.security.JwtAuthenticationFilter;
import lombok.RequiredArgsConstructor;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.http.HttpMethod;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;
import org.springframework.security.config.annotation.web.configurers.AbstractHttpConfigurer;
import org.springframework.security.config.http.SessionCreationPolicy;
import org.springframework.security.crypto.bcrypt.BCryptPasswordEncoder;
import org.springframework.security.crypto.password.PasswordEncoder;
import org.springframework.security.web.SecurityFilterChain;
import org.springframework.security.web.authentication.UsernamePasswordAuthenticationFilter;

@Configuration
@EnableWebSecurity
@RequiredArgsConstructor
public class SecurityConfig {

    private final JwtAuthenticationFilter jwtAuthenticationFilter;
    private final InternalApiKeyFilter internalApiKeyFilter;

    @Bean
    public SecurityFilterChain filterChain(HttpSecurity http) throws Exception {
        http
            .csrf(AbstractHttpConfigurer::disable)
            .sessionManagement(session -> 
                session.sessionCreationPolicy(SessionCreationPolicy.STATELESS))
            .authorizeHttpRequests(auth -> auth
                // Liveness/readiness probes must be reachable unauthenticated;
                // metrics (incl. prometheus) stay behind the internal API key.
                // The service's own OpenAPI document. Open because it describes only
                // the public surface (springdoc.paths-to-match), carries no data, and
                // the aggregated Swagger UI fetches it from the browser.
                .requestMatchers("/v3/api-docs/**").permitAll()
                .requestMatchers("/actuator/health/**", "/actuator/info").permitAll()
                .requestMatchers("/actuator/**").hasAuthority("INTERNAL")
                // Public routes (no auth required)
                .requestMatchers(HttpMethod.POST, "/api/auth/register/customer").permitAll()
                .requestMatchers(HttpMethod.POST, "/api/auth/register/seller").permitAll()
                .requestMatchers(HttpMethod.POST, "/api/auth/login").permitAll()
                .requestMatchers(HttpMethod.POST, "/api/auth/refresh").permitAll()
                
                // Internal routes - authorized backend services only per spec Section 6.
                // InternalApiKeyFilter grants INTERNAL on a valid X-Internal-Api-Key.
                .requestMatchers("/internal/**").hasAuthority("INTERNAL")
                
                // Authenticated routes
                .requestMatchers("/api/users/me").authenticated()
                .requestMatchers("/api/auth/logout").authenticated()
                
                // Admin routes
                .requestMatchers("/api/admin/**").hasAuthority("ADMIN")
                
                .anyRequest().authenticated()
            )
            // 401 for unauthenticated callers; the default entry point returns 403,
            // which misreports missing credentials as a permissions problem.
            // customer and seller already do this.
            .exceptionHandling(ex -> ex.authenticationEntryPoint(
                new org.springframework.security.web.authentication.HttpStatusEntryPoint(
                    org.springframework.http.HttpStatus.UNAUTHORIZED)))
            .addFilterBefore(jwtAuthenticationFilter, UsernamePasswordAuthenticationFilter.class)
            .addFilterBefore(internalApiKeyFilter, JwtAuthenticationFilter.class);

        return http.build();
    }

    @Bean
    public PasswordEncoder passwordEncoder() {
        return new BCryptPasswordEncoder(12);
    }
}
