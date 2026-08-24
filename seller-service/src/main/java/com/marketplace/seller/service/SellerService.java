package com.marketplace.seller.service;

import com.marketplace.seller.dto.SellerCreateRequest;
import com.marketplace.seller.dto.SellerResponse;
import com.marketplace.seller.dto.SellerUpdateRequest;
import com.marketplace.seller.exception.SellerAlreadyExistsException;
import com.marketplace.seller.exception.SellerNotFoundException;
import com.marketplace.seller.domain.Seller;
import com.marketplace.seller.repository.SellerRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.util.UUID;

@Service
@RequiredArgsConstructor
public class SellerService {

    private final SellerRepository sellerRepository;

    @Transactional
    public SellerResponse createSeller(SellerCreateRequest request) {
        if (sellerRepository.existsByUserId(request.userId())) {
            throw new SellerAlreadyExistsException("Seller profile already exists for this user");
        }

        Seller seller = Seller.builder()
                .userId(request.userId())
                .storeName(request.storeName())
                .description(request.description())
                .city(request.city())
                .address(request.address())
                .rating(0.0)
                .build();

        Seller saved = sellerRepository.save(seller);
        return mapToResponse(saved);
    }

    @Transactional(readOnly = true)
    public SellerResponse getSellerByUserId(UUID userId) {
        Seller seller = sellerRepository.findByUserId(userId)
                .orElseThrow(() -> new SellerNotFoundException("Seller not found for user: " + userId));
        return mapToResponse(seller);
    }

    @Transactional(readOnly = true)
    public SellerResponse getSellerById(UUID sellerId) {
        Seller seller = sellerRepository.findById(sellerId)
                .orElseThrow(() -> new SellerNotFoundException("Seller not found: " + sellerId));
        return mapToResponse(seller);
    }

    @Transactional
    public SellerResponse updateSeller(UUID userId, SellerUpdateRequest request) {
        Seller seller = sellerRepository.findByUserId(userId)
                .orElseThrow(() -> new SellerNotFoundException("Seller not found for user: " + userId));

        if (request.storeName() != null) seller.setStoreName(request.storeName());
        if (request.description() != null) seller.setDescription(request.description());
        if (request.city() != null) seller.setCity(request.city());
        if (request.address() != null) seller.setAddress(request.address());

        return mapToResponse(seller);
    }

    private SellerResponse mapToResponse(Seller seller) {
        return new SellerResponse(
                seller.getSellerId(),
                seller.getUserId(),
                seller.getStoreName(),
                seller.getDescription(),
                seller.getCity(),
                seller.getAddress(),
                seller.getRating(),
                seller.getCreatedAt(),
                seller.getUpdatedAt()
        );
    }
}
