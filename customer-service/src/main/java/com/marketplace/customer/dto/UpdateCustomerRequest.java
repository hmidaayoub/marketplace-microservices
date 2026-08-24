package com.marketplace.customer.dto;

import jakarta.validation.constraints.Size;
import lombok.Data;

@Data
public class UpdateCustomerRequest {

    @Size(max = 100, message = "First name must be less than 100 characters")
    private String firstName;

    @Size(max = 100, message = "Last name must be less than 100 characters")
    private String lastName;
}
