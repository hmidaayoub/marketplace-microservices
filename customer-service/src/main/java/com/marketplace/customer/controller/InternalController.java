package com.marketplace.customer.controller;

import com.marketplace.customer.dto.CustomerResponse;
import com.marketplace.customer.service.CustomerService;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.UUID;

@RestController
@RequestMapping("/internal/customers")
@RequiredArgsConstructor
public class InternalController {

    private final CustomerService customerService;

    @GetMapping("/{customerId}")
    public ResponseEntity<CustomerResponse> getCustomerById(@PathVariable UUID customerId) {
        return ResponseEntity.ok(customerService.getCustomerById(customerId));
    }

    @GetMapping("/by-user/{userId}")
    public ResponseEntity<CustomerResponse> getCustomerByUserId(@PathVariable UUID userId) {
        return ResponseEntity.ok(customerService.getCustomerByUserId(userId));
    }
}
