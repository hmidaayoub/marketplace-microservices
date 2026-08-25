package com.marketplace.customer.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.github.tomakehurst.wiremock.client.WireMock;
import com.marketplace.common.security.JwtUtil;
import com.marketplace.customer.AbstractIntegrationTest;
import com.marketplace.customer.dto.CustomerRequest;
import com.marketplace.customer.dto.UpdateCustomerRequest;
import com.marketplace.customer.repository.CustomerRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
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
    @Autowired JwtUtil jwtUtil;

    @Value("${internal.api.key}") String internalApiKey;

    private UUID userId;
    private String bearerToken;

    @BeforeEach
    void setUp() {
        customerRepository.deleteAll();
        userId = UUID.fromString("550e8400-e29b-41d4-a716-446655440000");
        bearerToken = "Bearer " + jwtUtil.generateAccessToken(userId, "test@example.com", "CUSTOMER");
        
        // Mock auth service to return 200 for this user
        WireMock.stubFor(WireMock.get(WireMock.urlEqualTo("/internal/users/" + userId))
                .willReturn(WireMock.aResponse()
                        .withStatus(200)
                        .withHeader("Content-Type", "application/json")
                        .withBody("{\"userId\":\"" + userId + "\",\"email\":\"test@example.com\",\"role\":\"CUSTOMER\"}")));
    }

    @Test
    @Transactional
    void createProfile_shouldReturn201_andCreateCustomer() throws Exception {
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Alice");
        request.setLastName("Smith");

        mockMvc.perform(post("/api/customers")
                .header("Authorization", bearerToken)
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
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated());

        // Second create should fail
        mockMvc.perform(post("/api/customers")
                .header("Authorization", bearerToken)
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
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andReturn().getResponse().getContentAsString();

        mockMvc.perform(get("/api/customers/me")
                .header("Authorization", bearerToken))
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
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(create)));

        // Update
        UpdateCustomerRequest update = new UpdateCustomerRequest();
        update.setFirstName("Charles");

        mockMvc.perform(put("/api/customers/me")
                .header("Authorization", bearerToken)
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
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andReturn().getResponse().getContentAsString();

        mockMvc.perform(get("/internal/customers/by-user/" + userId)
                .header("X-Internal-Api-Key", internalApiKey))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.firstName").value("Dave"));
    }

    @Test
    @Transactional
    void createProfile_shouldSendInternalApiKeyToAuthService() throws Exception {
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Frank");
        request.setLastName("Ng");

        mockMvc.perform(post("/api/customers")
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated());

        WireMock.verify(WireMock.getRequestedFor(
                WireMock.urlEqualTo("/internal/users/" + userId))
                .withHeader("X-Internal-Api-Key", WireMock.equalTo(internalApiKey)));
    }

    @Test
    @Transactional
    void createProfile_shouldReject_whenAuthUserIsNotACustomer() throws Exception {
        // auth-service reports this account as a SELLER
        WireMock.stubFor(WireMock.get(WireMock.urlEqualTo("/internal/users/" + userId))
                .willReturn(WireMock.aResponse()
                        .withStatus(200)
                        .withHeader("Content-Type", "application/json")
                        .withBody("{\"userId\":\"" + userId + "\",\"email\":\"s@example.com\",\"role\":\"SELLER\"}")));

        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Grace");
        request.setLastName("Lee");

        mockMvc.perform(post("/api/customers")
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isForbidden());
    }

    @Test
    void getMyProfile_shouldReturn401_whenNoCredentials() throws Exception {
        mockMvc.perform(get("/api/customers/me"))
            .andExpect(status().isUnauthorized());
    }

    @Test
    @Transactional
    void getMyProfile_shouldReturn401_whenOnlySpoofedUserIdHeader() throws Exception {
        CustomerRequest request = new CustomerRequest();
        request.setFirstName("Erin");
        request.setLastName("Hall");

        mockMvc.perform(post("/api/customers")
                .header("Authorization", bearerToken)
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated());

        // Erin's profile now exists, so before the fix this returned 200 with her
        // data. X-User-Id is caller-controlled and must not prove identity.
        mockMvc.perform(get("/api/customers/me")
                .header("X-User-Id", userId.toString()))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void internalGetCustomer_shouldReturn401_whenApiKeyMissing() throws Exception {
        mockMvc.perform(get("/internal/customers/by-user/" + userId))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void actuatorHealth_shouldBeReachableWithoutCredentials() throws Exception {
        mockMvc.perform(get("/actuator/health"))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("UP"));
    }

    @Test
    void internalGetCustomer_shouldReturn400_whenCustomerIdIsNotAUuid() throws Exception {
        mockMvc.perform(get("/internal/customers/not-a-uuid")
                .header("X-Internal-Api-Key", internalApiKey))
            .andExpect(status().isBadRequest());
    }

    @Test
    void internalGetCustomerByUser_shouldReturn400_whenUserIdIsNotAUuid() throws Exception {
        mockMvc.perform(get("/internal/customers/by-user/not-a-uuid")
                .header("X-Internal-Api-Key", internalApiKey))
            .andExpect(status().isBadRequest());
    }
}
