package com.marketplace.customer.controller;

import com.marketplace.customer.dto.CustomerRequest;
import com.marketplace.customer.dto.CustomerResponse;
import com.marketplace.customer.dto.UpdateCustomerRequest;
import com.marketplace.customer.service.CustomerService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.web.bind.annotation.*;

import java.util.UUID;

@RestController
@RequestMapping("/api/customers")
@RequiredArgsConstructor
public class CustomerController {

    private final CustomerService customerService;

    @PostMapping
    public ResponseEntity<CustomerResponse> createProfile(
            @AuthenticationPrincipal String userId,
            @Valid @RequestBody CustomerRequest request) {
        return ResponseEntity.status(HttpStatus.CREATED)
                .body(customerService.createProfile(UUID.fromString(userId), request));
    }

    @GetMapping("/me")
    public ResponseEntity<CustomerResponse> getMyProfile(
            @AuthenticationPrincipal String userId) {
        return ResponseEntity.ok(customerService.getMyProfile(UUID.fromString(userId)));
    }

    @PutMapping("/me")
    public ResponseEntity<CustomerResponse> updateMyProfile(
            @AuthenticationPrincipal String userId,
            @Valid @RequestBody UpdateCustomerRequest request) {
        return ResponseEntity.ok(customerService.updateMyProfile(UUID.fromString(userId), request));
    }
}
