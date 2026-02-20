---
title: Introduction
description: What is DiscoPanel and why use it.
---

DiscoPanel is a self-hosted Docker container management panel built for teams and individuals who want a clean, modern interface for managing their containerized infrastructure.

## Features

- **Container Management** - Start, stop, restart, and monitor Docker containers through an intuitive web UI
- **Role-Based Access Control** - Granular permissions system with customizable roles, scoped invites, and OIDC/OAuth2 support
- **Real-Time Streaming** - Live container logs and status updates via gRPC streaming
- **Module System** - Deploy pre-configured application stacks (databases, web servers, monitoring tools) with guided setup
- **API-First** - Full Connect/gRPC API with auto-generated OpenAPI documentation

## Architecture

DiscoPanel is composed of two main components:

- **Backend** - A Go server using Connect RPC (gRPC-compatible) that interfaces with the Docker daemon
- **Frontend** - A SvelteKit application with Svelte 5, communicating over Connect-Web

The backend communicates directly with the Docker socket to manage containers, images, networks, and volumes on your host.

## Next Steps

Head to the [Quick Start](/getting-started/quick-start/) guide to get DiscoPanel running in minutes.
