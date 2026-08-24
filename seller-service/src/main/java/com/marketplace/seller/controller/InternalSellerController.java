package com.marketplace.seller.controller;

import com.marketplace.seller.dto.SellerResponse;
import com.marketplace.seller.service.SellerService;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.UUID;

@RestController
@RequestMapping("/internal/sellers")
@RequiredArgsConstructor
public class InternalSellerController {

    private final SellerService sellerService;

    @GetMapping("/by-user/{userId}")
    public ResponseEntity<SellerResponse> getByUserId(@PathVariable UUID userId) {
        return ResponseEntity.ok(sellerService.getSellerByUserId(userId));
    }

    @GetMapping("/{sellerId}")
    public ResponseEntity<SellerResponse> getBySellerId(@PathVariable UUID sellerId) {
        return ResponseEntity.ok(sellerService.getSellerById(sellerId));
    }
}
