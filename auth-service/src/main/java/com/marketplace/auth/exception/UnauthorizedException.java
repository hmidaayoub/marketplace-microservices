package com.marketplace.auth.exception;

public class UnauthorizedException extends AuthException {
    public UnauthorizedException(String message) {
        super(message);
    }
}
