# Build context is the REPOSITORY ROOT, not this directory: the module needs the
# parent pom and the common-security module, both of which live above it.
#   docker build -f auth-service/Dockerfile .
# The root docker-compose.yml does this for you.
FROM maven:3.9.6-eclipse-temurin-21-alpine AS builder
WORKDIR /build

# Every module's pom is copied because the aggregator declares all of them and
# refuses to load if any is missing - even though -pl below builds only what
# this image needs. Poms first, so dependency resolution caches independently
# of source changes.
COPY pom.xml .
COPY common-security/pom.xml common-security/
COPY auth-service/pom.xml auth-service/
COPY customer-service/pom.xml customer-service/
COPY seller-service/pom.xml seller-service/
RUN mvn -B -pl common-security,auth-service -am dependency:go-offline

COPY common-security/src common-security/src
COPY auth-service/src auth-service/src
RUN mvn -B -pl common-security,auth-service -am clean package -DskipTests

FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
RUN addgroup -S marketplace && adduser -S auth -G marketplace
COPY --from=builder /build/auth-service/target/auth-service-*.jar app.jar
RUN chown auth:marketplace app.jar
USER auth
EXPOSE 8081
ENTRYPOINT ["java", "-XX:+UseContainerSupport", "-XX:MaxRAMPercentage=75.0", "-jar", "app.jar"]
