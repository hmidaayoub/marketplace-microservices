package com.marketplace.seller.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.marketplace.seller.dto.SellerCreateRequest;
import com.marketplace.seller.dto.SellerResponse;
import com.marketplace.seller.service.SellerService;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

import java.util.UUID;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@WebMvcTest(SellerController.class)
class SellerControllerTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @MockBean
    private SellerService sellerService;

    @Test
    void shouldCreateSeller() throws Exception {
        UUID userId = UUID.randomUUID();
        var request = new SellerCreateRequest(userId, "Store", "Desc", "City", "Addr");
        var response = new SellerResponse(UUID.randomUUID(), userId, "Store", "Desc", "City", "Addr", 0.0, null, null);

        when(sellerService.createSeller(any())).thenReturn(response);

        mockMvc.perform(post("/api/sellers")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
                .andExpect(status().isCreated())
                .andExpect(jsonPath("$.storeName").value("Store"));
    }

    @Test
    void shouldGetMyProfile() throws Exception {
        UUID userId = UUID.randomUUID();
        var response = new SellerResponse(UUID.randomUUID(), userId, "MyStore", "Desc", "City", "Addr", 0.0, null, null);

        when(sellerService.getSellerByUserId(userId)).thenReturn(response);

        mockMvc.perform(get("/api/sellers/me")
                .header("X-User-Id", userId.toString()))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.userId").value(userId.toString()));
    }

    @Test
    void shouldGetPublicProfile() throws Exception {
        UUID sellerId = UUID.randomUUID();
        var response = new SellerResponse(sellerId, UUID.randomUUID(), "PubStore", "Desc", "City", "Addr", 0.0, null, null);

        when(sellerService.getSellerById(sellerId)).thenReturn(response);

        mockMvc.perform(get("/api/sellers/{sellerId}", sellerId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.sellerId").value(sellerId.toString()));
    }
}
