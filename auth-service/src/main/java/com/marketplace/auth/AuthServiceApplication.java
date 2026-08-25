package com.marketplace.auth;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.ComponentScan;

@SpringBootApplication
// common.security holds the shared JWT and internal API-key filters; the default
// scan only covers this application's own package.
@ComponentScan(basePackages = {"com.marketplace.auth", "com.marketplace.common.security"})
public class AuthServiceApplication {
    public static void main(String[] args) {
        SpringApplication.run(AuthServiceApplication.class, args);
    }
}
