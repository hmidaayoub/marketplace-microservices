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
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.Optional;
import java.util.UUID;

import static org.assertj.core.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class CustomerServiceImplTest {

    @Mock CustomerRepository customerRepository;
    @Mock CustomerMapper customerMapper;
    @Mock AuthClient authClient;

    @InjectMocks CustomerServiceImpl customerService;

    private UUID userId;
    private CustomerRequest request;
    private Customer customer;

    @BeforeEach
    void setUp() {
        userId = UUID.randomUUID();
        request = new CustomerRequest();
        request.setFirstName("Alice");
        request.setLastName("Smith");

        customer = Customer.builder()
                .customerId(UUID.randomUUID())
                .userId(userId)
                .firstName("Alice")
                .lastName("Smith")
                .profileStatus(ProfileStatus.ACTIVE)
                .build();
    }

    @Test
    void createProfile_shouldCreateCustomer_whenUserExists() {
        when(customerRepository.existsByUserId(userId)).thenReturn(false);
        when(authClient.userExists(userId)).thenReturn(true);
        when(customerRepository.save(any(Customer.class))).thenReturn(customer);
        when(customerMapper.toResponse(any())).thenReturn(
            CustomerResponse.builder()
                .customerId(customer.getCustomerId())
                .userId(userId)
                .firstName("Alice")
                .lastName("Smith")
                .profileStatus(ProfileStatus.ACTIVE)
                .build()
        );

        CustomerResponse response = customerService.createProfile(userId, request);

        assertThat(response).isNotNull();
        assertThat(response.getFirstName()).isEqualTo("Alice");
        verify(customerRepository).save(any(Customer.class));
    }

    @Test
    void createProfile_shouldThrow_whenProfileAlreadyExists() {
        when(customerRepository.existsByUserId(userId)).thenReturn(true);

        assertThatThrownBy(() -> customerService.createProfile(userId, request))
                .isInstanceOf(CustomerAlreadyExistsException.class);
    }

    @Test
    void getMyProfile_shouldReturnCustomer_whenExists() {
        when(customerRepository.findByUserId(userId)).thenReturn(Optional.of(customer));
        when(customerMapper.toResponse(customer)).thenReturn(
            CustomerResponse.builder()
                .customerId(customer.getCustomerId())
                .userId(userId)
                .firstName("Alice")
                .lastName("Smith")
                .build()
        );

        CustomerResponse response = customerService.getMyProfile(userId);

        assertThat(response.getFirstName()).isEqualTo("Alice");
    }

    @Test
    void getMyProfile_shouldThrow_whenNotFound() {
        when(customerRepository.findByUserId(userId)).thenReturn(Optional.empty());

        assertThatThrownBy(() -> customerService.getMyProfile(userId))
                .isInstanceOf(CustomerNotFoundException.class);
    }

    @Test
    void updateMyProfile_shouldUpdateFields() {
        when(customerRepository.findByUserId(userId)).thenReturn(Optional.of(customer));
        when(customerRepository.save(any())).thenReturn(customer);
        when(customerMapper.toResponse(any())).thenReturn(
            CustomerResponse.builder()
                .customerId(customer.getCustomerId())
                .firstName("Alice Updated")
                .lastName("Smith")
                .build()
        );

        UpdateCustomerRequest update = new UpdateCustomerRequest();
        update.setFirstName("Alice Updated");

        CustomerResponse response = customerService.updateMyProfile(userId, update);

        assertThat(response.getFirstName()).isEqualTo("Alice Updated");
    }
}
