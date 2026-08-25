package com.marketplace.auth.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.marketplace.auth.AbstractIntegrationTest;
import com.marketplace.auth.dto.LoginRequest;
import com.marketplace.auth.dto.RegisterRequest;
import com.marketplace.auth.repository.UserRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.transaction.annotation.Transactional;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

class AuthControllerIntegrationTest extends AbstractIntegrationTest {

    private static final String INTERNAL_API_KEY_HEADER = "X-Internal-Api-Key";

    @Autowired MockMvc mockMvc;
    @Autowired ObjectMapper objectMapper;
    @Autowired UserRepository userRepository;

    @Value("${internal.api.key}") String internalApiKey;

    @BeforeEach
    void cleanUp() {
        userRepository.deleteAll();
    }

    @Test
    @Transactional
    void registerCustomer_shouldReturn201_andCreateUser() throws Exception {
        RegisterRequest request = new RegisterRequest();
        request.setEmail("customer@example.com");
        request.setPassword("password123");
        request.setPhoneNumber("+1234567890");

        mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated())
            .andExpect(jsonPath("$.email").value("customer@example.com"))
            .andExpect(jsonPath("$.role").value("CUSTOMER"));
    }

    @Test
    @Transactional
    void registerCustomer_shouldReturn409_whenDuplicateEmail() throws Exception {
        RegisterRequest request = new RegisterRequest();
        request.setEmail("dup@example.com");
        request.setPassword("password123");
        request.setPhoneNumber("+1111111111");

        mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isCreated());

        mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(request)))
            .andExpect(status().isConflict());
    }

    @Test
    @Transactional
    void login_shouldReturnTokens_whenValidCredentials() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("login@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+2222222222");

        mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andExpect(status().isCreated());

        LoginRequest login = new LoginRequest();
        login.setEmail("login@test.com");
        login.setPassword("password123");

        mockMvc.perform(post("/api/auth/login")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(login)))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.accessToken").exists())
            .andExpect(jsonPath("$.refreshToken").exists())
            .andExpect(jsonPath("$.tokenType").value("Bearer"));
    }

    @Test
    @Transactional
    void login_shouldReturn401_whenInvalidPassword() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("badlogin@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+3333333333");

        mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)));

        LoginRequest login = new LoginRequest();
        login.setEmail("badlogin@test.com");
        login.setPassword("wrongpassword");

        mockMvc.perform(post("/api/auth/login")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(login)))
            .andExpect(status().isUnauthorized());
    }

    @Test
    @Transactional
    void getMe_shouldReturnUser_whenAuthenticated() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("me@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+4444444444");

        String response = mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andReturn().getResponse().getContentAsString();

        LoginRequest login = new LoginRequest();
        login.setEmail("me@test.com");
        login.setPassword("password123");

        String loginResponse = mockMvc.perform(post("/api/auth/login")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(login)))
            .andReturn().getResponse().getContentAsString();

        String token = objectMapper.readTree(loginResponse).get("accessToken").asText();

        mockMvc.perform(get("/api/users/me")
                .header("Authorization", "Bearer " + token))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.email").value("me@test.com"));
    }

    @Test
    void internalGetUser_shouldReturnUser() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("internal@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+5555555555");

        String response = mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andExpect(status().isCreated())
            .andReturn().getResponse().getContentAsString();

        String userId = objectMapper.readTree(response).get("userId").asText();

        mockMvc.perform(get("/internal/users/" + userId)
                .header(INTERNAL_API_KEY_HEADER, internalApiKey))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.email").value("internal@test.com"));
    }

    @Test
    void internalGetUser_shouldNotExposePhoneNumber() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("nophone@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+7777777777");

        String response = mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andExpect(status().isCreated())
            .andReturn().getResponse().getContentAsString();

        String userId = objectMapper.readTree(response).get("userId").asText();

        mockMvc.perform(get("/internal/users/" + userId)
                .header(INTERNAL_API_KEY_HEADER, internalApiKey))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.userId").exists())
            .andExpect(jsonPath("$.email").value("nophone@test.com"))
            .andExpect(jsonPath("$.role").value("CUSTOMER"))
            .andExpect(jsonPath("$.phoneNumber").doesNotExist());
    }

    @Test
    void internalGetPhone_shouldReturnPhoneOnly() throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail("phone@test.com");
        reg.setPassword("password123");
        reg.setPhoneNumber("+6666666666");

        String response = mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andExpect(status().isCreated())
            .andReturn().getResponse().getContentAsString();

        String userId = objectMapper.readTree(response).get("userId").asText();

        mockMvc.perform(get("/internal/users/" + userId + "/phone")
                .header(INTERNAL_API_KEY_HEADER, internalApiKey))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.phoneNumber").value("+6666666666"));
    }

    @Test
    void internalGetPhone_shouldReturn401_whenApiKeyMissing() throws Exception {
        String userId = registerCustomerAndGetUserId("nokey-phone@test.com", "+8888888881");

        mockMvc.perform(get("/internal/users/" + userId + "/phone"))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void internalGetUser_shouldReturn401_whenApiKeyMissing() throws Exception {
        String userId = registerCustomerAndGetUserId("nokey-user@test.com", "+8888888882");

        mockMvc.perform(get("/internal/users/" + userId))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void internalGetUser_shouldReturn401_whenApiKeyWrong() throws Exception {
        String userId = registerCustomerAndGetUserId("badkey-user@test.com", "+8888888883");

        mockMvc.perform(get("/internal/users/" + userId)
                .header(INTERNAL_API_KEY_HEADER, "not-the-right-key"))
            .andExpect(status().isUnauthorized());
    }

    @Test
    void login_shouldReportExpiresIn_matchingConfiguredExpiry() throws Exception {
        registerCustomerAndGetUserId("expiry@test.com", "+9999999991");

        LoginRequest login = new LoginRequest();
        login.setEmail("expiry@test.com");
        login.setPassword("password123");

        // application-test.yml sets jwt.expiry-minutes: 15
        mockMvc.perform(post("/api/auth/login")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(login)))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.expiresIn").value(900));
    }

    @Test
    void actuatorHealth_shouldBeReachableWithoutCredentials() throws Exception {
        mockMvc.perform(get("/actuator/health"))
            .andExpect(status().isOk())
            .andExpect(jsonPath("$.status").value("UP"));
    }

    private String registerCustomerAndGetUserId(String email, String phoneNumber) throws Exception {
        RegisterRequest reg = new RegisterRequest();
        reg.setEmail(email);
        reg.setPassword("password123");
        reg.setPhoneNumber(phoneNumber);

        String response = mockMvc.perform(post("/api/auth/register/customer")
                .contentType(MediaType.APPLICATION_JSON)
                .content(objectMapper.writeValueAsString(reg)))
            .andExpect(status().isCreated())
            .andReturn().getResponse().getContentAsString();

        return objectMapper.readTree(response).get("userId").asText();
    }
}
