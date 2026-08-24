package com.marketplace.customer.exception;

public class CustomerAlreadyExistsException extends CustomerException {
    public CustomerAlreadyExistsException(String message) {
        super(message);
    }
}
