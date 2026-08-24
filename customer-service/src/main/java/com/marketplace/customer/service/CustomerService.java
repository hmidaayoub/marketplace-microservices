package com.marketplace.customer.service;

import com.marketplace.customer.dto.CustomerRequest;
import com.marketplace.customer.dto.CustomerResponse;
import com.marketplace.customer.dto.UpdateCustomerRequest;

import java.util.UUID;

public interface CustomerService {
    CustomerResponse createProfile(UUID userId, CustomerRequest request);
    CustomerResponse getMyProfile(UUID userId);
    CustomerResponse updateMyProfile(UUID userId, UpdateCustomerRequest request);
    CustomerResponse getCustomerById(UUID customerId);
    CustomerResponse getCustomerByUserId(UUID userId);
}
