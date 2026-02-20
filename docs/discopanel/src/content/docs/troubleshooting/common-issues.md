---
title: Common Issues
description: Solutions to frequently encountered problems.
---

## Docker Socket Permission Denied

**Symptom:** DiscoPanel starts but shows no containers, or logs show `permission denied` errors.

**Solution:** Ensure the Docker socket is mounted and accessible:

```yaml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock
```

On some systems, you may need to run the container with the `docker` group GID:

```yaml
group_add:
  - "${DOCKER_GID:-999}"
```

Find your Docker GID with: `getent group docker | cut -d: -f3`

## Cannot Connect to DiscoPanel

**Symptom:** Browser shows connection refused or timeout.

**Checklist:**
1. Verify the container is running: `docker ps | grep discopanel`
2. Check the port mapping matches your request URL
3. If behind a reverse proxy, verify the proxy is forwarding to the correct port
4. Check container logs: `docker logs discopanel`

## OIDC Login Not Working

**Symptom:** External login redirects fail or return errors.

**Checklist:**
1. Verify the redirect URI in your OIDC provider matches your DiscoPanel URL exactly
2. Ensure the client ID and secret are correct
3. Check that your provider's issuer URL is reachable from the DiscoPanel container
4. Review container logs for specific OIDC error messages

## Container Logs Not Streaming

**Symptom:** Log viewer opens but no output appears, or the stream disconnects.

**Possible Causes:**
- The container may not be producing output to stdout/stderr
- If behind a reverse proxy, ensure WebSocket/HTTP2 connections are not being terminated prematurely
- Check that your proxy supports streaming responses (some proxies buffer responses by default)
