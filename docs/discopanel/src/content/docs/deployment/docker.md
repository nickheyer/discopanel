---
title: Docker Deployment
description: Production deployment with Docker Compose.
---

## Basic Setup

The simplest way to deploy DiscoPanel is with Docker Compose. See the [Quick Start](/discopanel/getting-started/quick-start/) for the minimal configuration.

## Configuration

DiscoPanel is configured via a `config.yaml` file mounted into the container, or through environment variables.

### Volume Mounts

| Mount | Purpose |
|-------|---------|
| `/var/run/docker.sock` | **Required.** Access to the Docker daemon |
| `/app/data` | Persistent storage for database and settings |
| `/app/config.yaml` | Optional configuration file override |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DISCOPANEL_PORT` | `8080` | HTTP server port |

## Reverse Proxy

DiscoPanel works behind any reverse proxy. Here's an example with Traefik labels:

```yaml
services:
  discopanel:
    image: ghcr.io/nickheyer/discopanel:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.discopanel.rule=Host(`panel.example.com`)"
      - "traefik.http.services.discopanel.loadbalancer.server.port=8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - discopanel-data:/app/data
```

## OIDC / OAuth2

DiscoPanel supports external identity providers for authentication. Configure your OIDC provider in the admin settings panel or via `config.yaml`.
