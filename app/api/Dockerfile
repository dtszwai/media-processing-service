# syntax=docker/dockerfile:1.6

# Build stage
FROM maven:3.9.6-eclipse-temurin-21 AS build

WORKDIR /app

# Copy and build common module first
COPY common/pom.xml ./common/
COPY common/src ./common/src
RUN cd common && mvn clean install -DskipTests -B -q

# Copy and build generation providers module
COPY providers/pom.xml ./providers/
COPY providers/src ./providers/src
RUN cd providers && mvn clean install -DskipTests -B -q

# Copy API pom.xml for dependency caching
COPY api/pom.xml ./api/
WORKDIR /app/api
RUN mvn dependency:go-offline -B

# Copy API source code and build
COPY api/src ./src
RUN mvn clean package -DskipTests -B

# Runtime stage — Debian-based for glibc-built notebooklm-py wheels.
FROM eclipse-temurin:21-jre-jammy

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        curl python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r appgroup && useradd -r -g appgroup appuser

WORKDIR /app

COPY --from=scripts notebooklm/requirements.txt /tmp/notebooklm-requirements.txt
RUN python3 -m venv /opt/notebooklm-venv \
    && /opt/notebooklm-venv/bin/pip install --no-cache-dir --upgrade pip \
    && /opt/notebooklm-venv/bin/pip install --no-cache-dir -r /tmp/notebooklm-requirements.txt \
    && rm /tmp/notebooklm-requirements.txt

# Java artifact + provider bridge script
COPY --from=build /app/api/target/*.jar app.jar
COPY --from=scripts notebooklm/overview.py /app/scripts/notebooklm/overview.py

RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 9000

ENV JAVA_OPTS="-XX:+UseContainerSupport -XX:MaxRAMPercentage=75.0" \
    NOTEBOOKLM_PYTHON="/opt/notebooklm-venv/bin/python3" \
    NOTEBOOKLM_SCRIPT_PATH="/app/scripts/notebooklm/overview.py"

ENTRYPOINT ["sh", "-c", "java $JAVA_OPTS -jar app.jar"]
