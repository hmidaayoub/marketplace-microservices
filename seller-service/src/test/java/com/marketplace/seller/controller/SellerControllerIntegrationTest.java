package com.marketplace.seller.controller;

import com.github.tomakehurst.wiremock.client.WireMock;
import com.marketplace.common.security.JwtUtil;
import com.marketplace.seller.AbstractIntegrationTest;
import com.marketplace.seller.domain.Seller;
import com.marketplace.seller.repository.SellerRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.MediaType;
import org.springframework.cloud.contract.wiremock.AutoConfigureWireMock;
import org.springframework.test.web.servlet.MockMvc;

import java.util.UUID;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

@AutoConfigureWireMock(port = 0)
class SellerControllerIntegrationTest extends AbstractIntegrationTest {

    @Autowired MockMvc mockMvc;
    @Autowired SellerRepository sellerRepository;
    @Autowired JwtUtil jwtUtil;

    @Value("${internal.api.key}") String internalApiKey;

    private UUID userId;
    private UUID sellerId;
    private String bearerToken;

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
        bearerToken = "Bearer " + jwtUtil.generateAccessToken(userId, "seller@test.com", "SELLER");
        stubAuthRole(userId, "SELLER");
    }

    @Test
    void myProfile_shouldReturn401_whenNoCredentials() throws Exception {
        mockMvc.perform(get("/api/sellers/me"))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void myProfile_shouldReturn401_whenOnlySpoofedUserIdHeader() throws Exception {
        // The seller exists, so before the fix this returned 200 with their data.
        mockMvc.perform(get("/api/sellers/me")
                .header("X-User-Id", userId.toString()))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void createSeller_shouldReturn401_whenUnauthenticated() throws Exception {
        mockMvc.perform(post("/api/sellers")
                .contentType(org.springframework.http.MediaType.APPLICATION_JSON)
                .content("{\"userId\":\"" + UUID.randomUUID() + "\",\"storeName\":\"Spoofed\"}"))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void internalGetSeller_shouldReturn401_whenApiKeyMissing() throws Exception {
        mockMvc.perform(get("/internal/sellers/by-user/" + userId))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void myProfile_shouldReturnSeller_whenAuthenticated() throws Exception {
        mockMvc.perform(get("/api/sellers/me")
                .header("Authorization", bearerToken))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.storeName").value("Testcontainers Store"));
    }

    @Test
    void internalGetSeller_shouldReturnSeller_whenApiKeyPresent() throws Exception {
        mockMvc.perform(get("/internal/sellers/by-user/" + userId)
                .header("X-Internal-Api-Key", internalApiKey))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.storeName").value("Testcontainers Store"));
    }

    @Test
    void createSeller_shouldUseTokenIdentity_andIgnoreSpoofedBodyUserId() throws Exception {
        UUID authenticatedUser = UUID.randomUUID();
        UUID spoofedUser = UUID.randomUUID();
        String token = "Bearer " + jwtUtil.generateAccessToken(authenticatedUser, "new@test.com", "SELLER");
        stubAuthRole(authenticatedUser, "SELLER");

        mockMvc.perform(post("/api/sellers")
                .header("Authorization", token)
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"userId\":\"" + spoofedUser + "\",\"storeName\":\"New Store\"}"))
            .andExpect(status().isCreated())
            .andExpect(jsonPath("$.storeName").value("New Store"))
            // the profile belongs to the token subject, not the id in the body
            .andExpect(jsonPath("$.userId").value(authenticatedUser.toString()));
    }

    @Test
    void publicProfile_shouldReturnSeller() throws Exception {
        mockMvc.perform(get("/api/sellers/" + sellerId))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.storeName").value("Testcontainers Store"));
    }

    @Test
    void publicProfile_shouldNotExposeAddressOrUserId() throws Exception {
        mockMvc.perform(get("/api/sellers/" + sellerId))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.storeName").value("Testcontainers Store"))
            .andExpect(jsonPath("$.city").value("Tunis"))
            .andExpect(jsonPath("$.address").doesNotExist())
            .andExpect(jsonPath("$.userId").doesNotExist());
    }

    @Test
    void myProfile_shouldStillExposeAddress() throws Exception {
        mockMvc.perform(get("/api/sellers/me")
                .header("Authorization", bearerToken))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.address").value("12 Rue Example"));
    }

    @Test
    void actuatorHealth_shouldBeReachableWithoutCredentials() throws Exception {
        mockMvc.perform(get("/actuator/health"))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("UP"));
    }

    @Test
    void createSeller_shouldReject_whenAuthUserIsNotASeller() throws Exception {
        UUID customerUser = UUID.randomUUID();
        String token = "Bearer " + jwtUtil.generateAccessToken(customerUser, "c@test.com", "CUSTOMER");
        stubAuthRole(customerUser, "CUSTOMER");

        mockMvc.perform(post("/api/sellers")
                .header("Authorization", token)
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"storeName\":\"Not A Seller\"}"))
            .andExpect(status().isForbidden());
    }

    @Test
    void createSeller_shouldSendInternalApiKeyToAuthService() throws Exception {
        UUID newUser = UUID.randomUUID();
        String token = "Bearer " + jwtUtil.generateAccessToken(newUser, "k@test.com", "SELLER");
        stubAuthRole(newUser, "SELLER");

        mockMvc.perform(post("/api/sellers")
                .header("Authorization", token)
                .contentType(MediaType.APPLICATION_JSON)
                .content("{\"storeName\":\"Keyed Store\"}"))
            .andExpect(status().isCreated());

        WireMock.verify(WireMock.getRequestedFor(WireMock.urlEqualTo("/internal/users/" + newUser))
                .withHeader("X-Internal-Api-Key", WireMock.equalTo(internalApiKey)));
    }

    @Test
    void publicProfile_shouldReturn400_whenSellerIdIsNotAUuid() throws Exception {
        mockMvc.perform(get("/api/sellers/not-a-uuid"))
            .andExpect(status().isBadRequest());
    }

    @Test
    void internalGetSeller_shouldReturn400_whenSellerIdIsNotAUuid() throws Exception {
        mockMvc.perform(get("/internal/sellers/not-a-uuid")
                .header("X-Internal-Api-Key", internalApiKey))
            .andExpect(status().isBadRequest());
    }

    private void stubAuthRole(UUID user, String role) {
        WireMock.stubFor(WireMock.get(WireMock.urlEqualTo("/internal/users/" + user))
                .willReturn(WireMock.aResponse()
                        .withStatus(200)
                        .withHeader("Content-Type", "application/json")
                        .withBody("{\"userId\":\"" + user + "\",\"role\":\"" + role + "\"}")));
    }

}
