# Build stage
FROM maven:3.9.6-eclipse-temurin-21 AS build

WORKDIR /app

# Copy and build common module first
COPY common/pom.xml ./common/
COPY common/src ./common/src
RUN cd common && mvn clean install -DskipTests -B -q

# Copy API pom.xml for dependency caching
COPY api/pom.xml ./api/
WORKDIR /app/api
RUN mvn dependency:go-offline -B

# Copy API source code and build
COPY api/src ./src
RUN mvn clean package -DskipTests -B

# Runtime stage
FROM eclipse-temurin:21-jre-alpine

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy the built JAR from build stage
COPY --from=build /app/api/target/*.jar app.jar

# Set ownership
RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 9000

# OpenTelemetry Java agent for auto-instrumentation (optional)
ENV JAVA_OPTS="-XX:+UseContainerSupport -XX:MaxRAMPercentage=75.0"

ENTRYPOINT ["sh", "-c", "java $JAVA_OPTS -jar app.jar"]
