package com.marketplace.seller.service;

import com.marketplace.seller.dto.SellerCreateRequest;
import com.marketplace.seller.dto.SellerResponse;
import com.marketplace.seller.dto.SellerUpdateRequest;
import com.marketplace.seller.exception.SellerAlreadyExistsException;
import com.marketplace.seller.exception.SellerNotFoundException;
import com.marketplace.seller.domain.Seller;
import com.marketplace.seller.repository.SellerRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.Optional;
import java.util.UUID;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class SellerServiceTest {

    @Mock
    private SellerRepository sellerRepository;

    @InjectMocks
    private SellerService sellerService;

    private UUID userId;
    private UUID sellerId;
    private Seller seller;

    @BeforeEach
    void setUp() {
        userId = UUID.randomUUID();
        sellerId = UUID.randomUUID();

        seller = Seller.builder()
                .sellerId(sellerId)
                .userId(userId)
                .storeName("TechStore")
                .description("Gadgets")
                .city("Berlin")
                .address("Alexanderplatz 1")
                .rating(0.0)
                .build();
    }

    @Test
    void shouldCreateSellerProfile() {
        var request = new SellerCreateRequest(userId, "TechStore", "Gadgets", "Berlin", "Alexanderplatz 1");

        when(sellerRepository.existsByUserId(userId)).thenReturn(false);
        when(sellerRepository.save(any(Seller.class))).thenAnswer(i -> {
            Seller s = i.getArgument(0);
            s.setSellerId(sellerId);
            return s;
        });

        SellerResponse response = sellerService.createSeller(request);

        assertThat(response.storeName()).isEqualTo("TechStore");
        assertThat(response.userId()).isEqualTo(userId);
        verify(sellerRepository).save(any(Seller.class));
    }

    @Test
    void shouldRejectDuplicateUserId() {
        var request = new SellerCreateRequest(userId, "Store", "Desc", "City", "Addr");
        when(sellerRepository.existsByUserId(userId)).thenReturn(true);

        assertThatThrownBy(() -> sellerService.createSeller(request))
                .isInstanceOf(SellerAlreadyExistsException.class)
                .hasMessageContaining("already exists");
    }

    @Test
    void shouldReturnSellerByUserId() {
        when(sellerRepository.findByUserId(userId)).thenReturn(Optional.of(seller));

        SellerResponse response = sellerService.getSellerByUserId(userId);

        assertThat(response.sellerId()).isEqualTo(sellerId);
        assertThat(response.storeName()).isEqualTo("TechStore");
    }

    @Test
    void shouldThrowWhenSellerNotFoundByUserId() {
        when(sellerRepository.findByUserId(userId)).thenReturn(Optional.empty());

        assertThatThrownBy(() -> sellerService.getSellerByUserId(userId))
                .isInstanceOf(SellerNotFoundException.class);
    }

    @Test
    void shouldUpdateOwnProfile() {
        var update = new SellerUpdateRequest("NewName", "NewDesc", "Munich", "Marienplatz 2");
        when(sellerRepository.findByUserId(userId)).thenReturn(Optional.of(seller));

        SellerResponse response = sellerService.updateSeller(userId, update);

        assertThat(response.storeName()).isEqualTo("NewName");
        assertThat(response.city()).isEqualTo("Munich");
    }

    @Test
    void shouldReturnPublicProfileBySellerId() {
        when(sellerRepository.findById(sellerId)).thenReturn(Optional.of(seller));

        SellerResponse response = sellerService.getSellerById(sellerId);

        assertThat(response.sellerId()).isEqualTo(sellerId);
    }
}
