package com.marketplace.seller.controller;

import com.marketplace.seller.AbstractIntegrationTest;
import com.marketplace.seller.domain.Seller;
import com.marketplace.seller.repository.SellerRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.web.servlet.MockMvc;

import java.util.UUID;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

class SellerControllerIntegrationTest extends AbstractIntegrationTest {

    @Autowired MockMvc mockMvc;
    @Autowired SellerRepository sellerRepository;

    private UUID userId;
    private UUID sellerId;

    @BeforeEach
    void setUp() {
        sellerRepository.deleteAll();
        userId = UUID.randomUUID();
        Seller seller = sellerRepository.save(Seller.builder()
                .userId(userId)
                .storeName("Testcontainers Store")
                .description("Runs against real Postgres")
                .city("Tunis")
                .address("12 Rue Example")
                .rating(0.0)
                .build());
        sellerId = seller.getSellerId();
    }

    @Test
    void publicProfile_shouldReturnSeller() throws Exception {
        mockMvc.perform(get("/api/sellers/" + sellerId))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.storeName").value("Testcontainers Store"));
    }
}
