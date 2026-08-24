package com.marketplace.customer.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.github.tomakehurst.wiremock.client.WireMock;
import com.marketplace.customer.AbstractIntegrationTest;
import com.marketplace.customer.dto.CustomerRequest;
import com.marketplace.customer.dto.UpdateCustomerRequest;
import com.marketplace.customer.repository.CustomerRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.cloud.contract.wiremock.AutoConfigureWireMock;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.transaction.annotation.Transactional;

import java.util.UUID;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@AutoConfigureWireMock(port = 0)
class CustomerControllerIntegrationTest extends AbstractIntegrationTest {

    @Autowired MockMvc mockMvc;
    @Autowired ObjectMapper objectMapper;
    @Autowired CustomerRepository customerRepository;

    private UUID userId;

    @BeforeEach
    void setUp() {
        customerRepository.deleteAll();
        userId = UUID.fromString("550e8400-e29b-41d4-a716-446655440000");
        
        // Mock auth service to return 200 for this user
        WireMock.stubFor(WireMock.get(WireMock.urlEqualTo("/internal/users/" + userId))
                .willReturn(WireMock.aResponse()
                        .withStatus(200)
                        .withHeader("Content-Type", "application/json")
                        .withBody("{\"userId\":\"" + userId + "\",\"email\":\"test@example.com\"}")));
    }

    @Test
    @Transactional
    void createProfile_shouldReturn201_andCreateCustomer() throws Exception {
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Alice");
        request.setLastName("Smith");

        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated())
            .andExpect(jsonPath("$.firstName").value("Alice"))
            .andExpect(jsonPath("$.lastName").value("Smith"))
            .andExpect(jsonPath("$.profileStatus").value("ACTIVE"));
    }

    @Test
    @Transactional
    void createProfile_shouldReturn409_whenDuplicate() throws Exception {
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Alice");
        request.setLastName("Smith");

        // First create
        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated());

        // Second create should fail
        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isConflict());
    }

    @Test
    @Transactional
    void getMyProfile_shouldReturnCustomer() throws Exception {
        // First create
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Bob");
        request.setLastName("Jones");

        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andReturn().getResponse().getContentAsString();

        mockMvc.perform(get("/api/customers/me")
                .header("X-User-Id", userId.toString()))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.firstName").value("Bob"))
            .andExpect(jsonPath("$.lastName").value("Jones"));
    }

    @Test
    @Transactional
    void updateMyProfile_shouldUpdateFields() throws Exception {
        // Create first
        CustomerRequest create = new CustomerRequest();
        create.setFirstName("Charlie");
        create.setLastName("Brown");

        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(create)));

        // Update
        UpdateCustomerRequest update = new UpdateCustomerRequest();
        update.setFirstName("Charles");

        mockMvc.perform(put("/api/customers/me")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(update)))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.firstName").value("Charles"))
            .andExpect(jsonPath("$.lastName").value("Brown"));
    }

    @Test
    void internalGetCustomerByUserId_shouldReturnCustomer() throws Exception {
        // Create first
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Dave");
        request.setLastName("Wilson");

        mockMvc.perform(post("/api/customers")
                .header("X-User-Id", userId.toString())
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andReturn().getResponse().getContentAsString();

        mockMvc.perform(get("/internal/customers/by-user/" + userId))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.firstName").value("Dave"));
    }
}
