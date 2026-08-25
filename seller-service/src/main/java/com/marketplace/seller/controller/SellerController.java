package com.marketplace.seller.controller;

import com.marketplace.seller.dto.SellerCreateRequest;
import com.marketplace.seller.dto.SellerResponse;
import com.marketplace.seller.dto.SellerUpdateRequest;
import com.marketplace.seller.service.SellerService;
import jakarta.validation.Valid;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.web.bind.annotation.*;

import java.util.UUID;

@RestController
@RequestMapping("/api/sellers")
@RequiredArgsConstructor
public class SellerController {

    private final SellerService sellerService;

    @PostMapping
    public ResponseEntity<SellerResponse> createSeller(
            @AuthenticationPrincipal String userId,
            @Valid @RequestBody SellerCreateRequest request) {
        return ResponseEntity.status(HttpStatus.CREATED)
                .body(sellerService.createSeller(UUID.fromString(userId), request));
    }

    @GetMapping("/me")
    public ResponseEntity<SellerResponse> getMyProfile(@AuthenticationPrincipal String userIdRaw) {
        return ResponseEntity.ok(sellerService.getSellerByUserId(UUID.fromString(userIdRaw)));
    }

    @PutMapping("/me")
    public ResponseEntity<SellerResponse> updateMyProfile(
            @AuthenticationPrincipal String userIdRaw,
            @RequestBody SellerUpdateRequest request) {
        return ResponseEntity.ok(sellerService.updateSeller(UUID.fromString(userIdRaw), request));
    }

    @GetMapping("/{sellerId}")
    public ResponseEntity<SellerResponse> getPublicProfile(@PathVariable UUID sellerId) {
        return ResponseEntity.ok(sellerService.getSellerById(sellerId));
    }
}
