package com.marketplace.common.security;

import io.swagger.v3.oas.models.Components;
import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.info.Info;
import io.swagger.v3.oas.models.security.SecurityRequirement;
import io.swagger.v3.oas.models.security.SecurityScheme;
import io.swagger.v3.oas.models.servers.Server;
import java.util.List;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * The OpenAPI document every Spring service publishes.
 *
 * <p>It lives in common-security because all three Java services already scan this
 * package, so one definition keeps their specs identical rather than three that drift.
 * The Go and Python services describe themselves the same way - bearer auth, a relative
 * server - so the aggregated Swagger UI behaves the same whichever spec is selected.
 */
@Configuration
public class OpenApiConfig {

    private static final String BEARER_SCHEME = "bearerAuth";

    @Value("${spring.application.name:service}")
    private String applicationName;

    @Bean
    public OpenAPI openAPI() {
        return new OpenAPI()
            // Relative, so the browser resolves it against whatever origin served the
            // spec - the gateway on 8080. An absolute URL derived from the request
            // would name the container's own port, which nothing can reach.
            .servers(List.of(new Server().url("/")))
            .info(new Info()
                .title(applicationName)
                .version("1.0.0")
                .description("Public API. The /internal endpoints are deliberately absent: "
                    + "they are service-to-service only and have no route through the gateway."))
            // Declared once and applied to every operation, because every public
            // endpoint outside the four open auth routes needs the token. Swagger UI
            // turns this into the Authorize button.
            .components(new Components().addSecuritySchemes(BEARER_SCHEME,
                new SecurityScheme()
                    .type(SecurityScheme.Type.HTTP)
                    .scheme("bearer")
                    .bearerFormat("JWT")
                    .description("Paste the accessToken from POST /api/auth/login.")))
            .addSecurityItem(new SecurityRequirement().addList(BEARER_SCHEME));
    }
}
