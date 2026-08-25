package com.marketplace.customer.config;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.client.RestTemplate;

@Configuration
public class RestTemplateConfig {

    /** Header the internal APIs authenticate service-to-service calls with (spec section 6). */
    public static final String INTERNAL_API_KEY_HEADER = "X-Internal-Api-Key";

    /**
     * Attaches the shared internal API key to every outgoing call, so any future internal
     * client inherits it instead of each one having to remember the header.
     */
    @Bean
    public RestTemplate restTemplate(@Value("${internal.api.key:}") String internalApiKey) {
        RestTemplate restTemplate = new RestTemplate();
        restTemplate.getInterceptors().add((request, body, execution) -> {
            request.getHeaders().add(INTERNAL_API_KEY_HEADER, internalApiKey);
            return execution.execute(request, body);
        });
        return restTemplate;
    }
}
