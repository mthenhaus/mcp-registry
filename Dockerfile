FROM chainguard/bash:latest

LABEL name="Anywhere.re MCP Registry"

# Set working directory
WORKDIR /app

RUN mkdir -p /tmp/data && chmod 777 /tmp/data

# Copy the registry binary
COPY bin/registry ./
RUN chmod +x ./registry

# Copy initial data files
COPY data /tmp/data

# Expose port 8080
EXPOSE 8080

# Set the entrypoint to the registry binary
ENTRYPOINT ["./registry"]