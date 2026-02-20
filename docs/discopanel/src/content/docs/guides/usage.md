---
title: Usage Guide
description: Managing containers and using DiscoPanel features.
---

## Dashboard

The dashboard provides an overview of all containers running on your Docker host, including status, resource usage, and quick actions.

## Container Management

### Viewing Containers

The container list shows all containers (running and stopped) with their current status, image, ports, and uptime.

### Container Actions

From the container detail view, you can:

- **Start / Stop / Restart** - Control the container lifecycle
- **View Logs** - Stream real-time logs with search and filtering
- **Inspect** - View container configuration, environment variables, and mounts

## Module System

Modules are pre-configured application stacks that can be deployed through a guided setup wizard.

### Installing a Module

1. Navigate to the **Modules** section
2. Browse or search for available modules
3. Click a module to view its details and configuration options
4. Follow the setup wizard to configure and deploy

### Module Configuration

Each module exposes configurable parameters (ports, volumes, environment variables) through a form-based interface. Defaults are provided but can be customized.

## User Management

### Roles & Permissions

DiscoPanel uses role-based access control (RBAC). Roles can be created with specific permissions scoped to:

- Container operations (view, start, stop, delete)
- Module management (install, configure, remove)
- User administration (invite, assign roles)
- System settings

### Inviting Users

Admins can generate scoped invite links that pre-assign roles to new users upon registration.
