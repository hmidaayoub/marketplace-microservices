# Build context is the REPOSITORY ROOT, not this directory: the module needs the
# parent pom and the common-security module, both of which live above it.
#   docker build -f customer-service/Dockerfile .
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
RUN mvn -B -pl common-security,customer-service -am dependency:go-offline

COPY common-security/src common-security/src
COPY customer-service/src customer-service/src
RUN mvn -B -pl common-security,customer-service -am clean package -DskipTests

FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
RUN addgroup -S marketplace && adduser -S customer -G marketplace
COPY --from=builder /build/customer-service/target/customer-service-*.jar app.jar
RUN chown customer:marketplace app.jar
USER customer
EXPOSE 8082
ENTRYPOINT ["java", "-XX:+UseContainerSupport", "-XX:MaxRAMPercentage=75.0", "-jar", "app.jar"]
