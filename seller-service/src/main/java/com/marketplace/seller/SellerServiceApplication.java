package com.marketplace.seller;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.ComponentScan;

@SpringBootApplication
@ComponentScan(basePackages = {"com.marketplace.seller", "com.marketplace.common.security"})
public class SellerServiceApplication {
    public static void main(String[] args) {
        SpringApplication.run(SellerServiceApplication.class, args);
    }
}