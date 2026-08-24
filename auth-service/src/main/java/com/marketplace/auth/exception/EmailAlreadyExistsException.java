package com.marketplace.auth.exception;

public class EmailAlreadyExistsException extends AuthException {
    public EmailAlreadyExistsException(String email) {
        super("Email already registered: " + email);
    }
}
