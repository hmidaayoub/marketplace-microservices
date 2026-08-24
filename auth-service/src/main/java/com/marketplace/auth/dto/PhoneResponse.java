package com.marketplace.auth.dto;

import lombok.Builder;
import lombok.Data;

@Data
@Builder
public class PhoneResponse {
    private String phoneNumber;
}
