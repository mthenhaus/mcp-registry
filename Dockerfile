FROM chainguard/bash:latest

LABEL name="Anywhere.re MCP Registry"

# Set working directory
WORKDIR /app

RUN mkdir -p /tmp/data && chmod 777 /tmp/data

# Copy the registry binary
COPY bin/registry ./
RUN chmod +x ./registry

# Copy initial data files
COPY data/registry.json /tmp/data/registry.json
COPY data/seed.json /tmp/data/seed.json

# Expose port 8080
EXPOSE 8080

# Set the entrypoint to the registry binary
ENTRYPOINT ["./registry"]