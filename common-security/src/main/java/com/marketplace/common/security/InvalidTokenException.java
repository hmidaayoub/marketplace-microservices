package com.marketplace.common.security;

/**
 * Raised when a JWT is malformed, expired or fails signature verification.
 * Typed so services can map it to 401 instead of falling through a generic
 * handler and reporting 500.
 */
public class InvalidTokenException extends RuntimeException {
    public InvalidTokenException(String message) {
        super(message);
    }
}
