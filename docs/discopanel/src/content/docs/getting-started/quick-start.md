---
title: Quick Start
description: Get DiscoPanel running in minutes.
---

## Prerequisites

- Docker and Docker Compose installed on your host
- The Docker socket accessible at `/var/run/docker.sock`

## Docker Compose (Recommended)

Create a `docker-compose.yml`:

```yaml
services:
  discopanel:
    image: ghcr.io/nickheyer/discopanel:latest
    container_name: discopanel
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - discopanel-data:/app/data

volumes:
  discopanel-data:
```

Then start it:

```bash
docker compose up -d
```

DiscoPanel will be available at `http://localhost:8080`.

## First Login

On first launch, you'll be prompted to create an admin account. This account has full access to all DiscoPanel features.

## Next Steps

- [Deployment Guide](/deployment/docker/) - Production deployment options and configuration
- [Usage Guide](/guides/usage/) - Learn how to manage containers and use the module system
