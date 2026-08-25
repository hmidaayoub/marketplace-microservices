package com.marketplace.auth.config;

import com.marketplace.auth.domain.Role;
import com.marketplace.auth.domain.User;
import com.marketplace.auth.domain.UserStatus;
import com.marketplace.auth.repository.UserRepository;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.boot.ApplicationRunner;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.security.crypto.password.PasswordEncoder;

/**
 * Seeds the single ADMIN account. The spec defines ADMIN as a role but no
 * registration endpoint for it, deliberately: admins are provisioned, not
 * self-registered. Without this the approval flows (spec sections 16-17)
 * cannot be exercised at all.
 *
 * Disable with admin.bootstrap.enabled=false.
 */
@Slf4j
@Configuration
@RequiredArgsConstructor
@ConditionalOnProperty(value = "admin.bootstrap.enabled", havingValue = "true", matchIfMissing = true)
public class AdminBootstrap {

    @Bean
    public ApplicationRunner seedAdminUser(
            UserRepository userRepository,
            PasswordEncoder passwordEncoder,
            org.springframework.core.env.Environment env) {

        return args -> {
            String email = env.getProperty("admin.bootstrap.email", "admin@marketplace.local");
            String password = env.getProperty("admin.bootstrap.password");
            String phone = env.getProperty("admin.bootstrap.phone", "+10000000000");

            if (password == null || password.isBlank()) {
                log.warn("admin.bootstrap.password is not set - skipping ADMIN seeding");
                return;
            }

            if (userRepository.existsByEmail(email)) {
                log.debug("ADMIN account already present: {}", email);
                return;
            }

            userRepository.save(User.builder()
                    .email(email)
                    .phoneNumber(phone)
                    .passwordHash(passwordEncoder.encode(password))
                    .role(Role.ADMIN)
                    .status(UserStatus.ACTIVE)
                    .build());

            log.info("Seeded ADMIN account: {}", email);
        };
    }
}
