package com.marketplace.seller.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.marketplace.seller.dto.SellerCreateRequest;
import com.marketplace.seller.dto.SellerPublicResponse;
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
import static org.springframework.security.test.web.servlet.request.SecurityMockMvcRequestPostProcessors.user;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@WebMvcTest(controllers = SellerController.class,
    excludeAutoConfiguration = org.springframework.boot.autoconfigure.security.servlet.SecurityAutoConfiguration.class)
@org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc(addFilters = false)
class SellerControllerTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ObjectMapper objectMapper;

    @MockBean
    private SellerService sellerService;



    @Test
    void shouldGetPublicProfile() throws Exception {
        UUID sellerId = UUID.randomUUID();
        var response = new SellerPublicResponse(sellerId, "PubStore", "Desc", "City", 0.0, null);

        when(sellerService.getPublicSellerById(sellerId)).thenReturn(response);

        mockMvc.perform(get("/api/sellers/{sellerId}", sellerId))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.sellerId").value(sellerId.toString()));
    }
}
