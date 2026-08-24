package com.marketplace.auth.exception;

public class PhoneAlreadyExistsException extends AuthException {
    public PhoneAlreadyExistsException(String phone) {
        super("Phone number already registered: " + phone);
    }
}
