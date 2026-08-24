package com.marketplace.customer.service;

import com.marketplace.customer.client.AuthClient;
import com.marketplace.customer.domain.Customer;
import com.marketplace.customer.domain.ProfileStatus;
import com.marketplace.customer.dto.CustomerRequest;
import com.marketplace.customer.dto.CustomerResponse;
import com.marketplace.customer.dto.UpdateCustomerRequest;
import com.marketplace.customer.exception.CustomerAlreadyExistsException;
import com.marketplace.customer.exception.CustomerNotFoundException;
import com.marketplace.customer.mapper.CustomerMapper;
import com.marketplace.customer.repository.CustomerRepository;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.UUID;

@Slf4j
@Service
@RequiredArgsConstructor
@Transactional
public class CustomerServiceImpl implements CustomerService {

    private final CustomerRepository customerRepository;
    private final CustomerMapper customerMapper;
    private final AuthClient authClient;

    @Override
    public CustomerResponse createProfile(UUID userId, CustomerRequest request) {
        if (customerRepository.existsByUserId(userId)) {
            throw new CustomerAlreadyExistsException("Customer profile already exists for this user");
        }

        authClient.userExists(userId);

        Customer customer = Customer.builder()
                .userId(userId)
                .firstName(request.getFirstName())
                .lastName(request.getLastName())
                .profileStatus(ProfileStatus.ACTIVE)
                .build();

        Customer saved = customerRepository.save(customer);
        log.info("Created customer profile: customerId={}, userId={}", saved.getCustomerId(), userId);
        return customerMapper.toResponse(saved);
    }

    @Override
    @Transactional(readOnly = true)
    public CustomerResponse getMyProfile(UUID userId) {
        Customer customer = customerRepository.findByUserId(userId)
                .orElseThrow(() -> new CustomerNotFoundException("Customer profile not found"));
        return customerMapper.toResponse(customer);
    }

    @Override
    public CustomerResponse updateMyProfile(UUID userId, UpdateCustomerRequest request) {
        Customer customer = customerRepository.findByUserId(userId)
                .orElseThrow(() -> new CustomerNotFoundException("Customer profile not found"));

        if (request.getFirstName() != null) {
            customer.setFirstName(request.getFirstName());
        }
        if (request.getLastName() != null) {
            customer.setLastName(request.getLastName());
        }

        Customer updated = customerRepository.save(customer);
        return customerMapper.toResponse(updated);
    }

    @Override
    @Transactional(readOnly = true)
    public CustomerResponse getCustomerById(UUID customerId) {
        Customer customer = customerRepository.findById(customerId)
                .orElseThrow(() -> new CustomerNotFoundException("Customer not found: " + customerId));
        return customerMapper.toResponse(customer);
    }

    @Override
    @Transactional(readOnly = true)
    public CustomerResponse getCustomerByUserId(UUID userId) {
        Customer customer = customerRepository.findByUserId(userId)
                .orElseThrow(() -> new CustomerNotFoundException("Customer not found for user: " + userId));
        return customerMapper.toResponse(customer);
    }
}
