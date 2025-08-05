# Testing Proxy on Localhost

This guide explains how to test the DiscoPanel proxy feature on your local machine.

## Configuration

Use the provided `config.localhost.yaml` or modify your config with these settings:

```yaml
proxy:
  enabled: true
  base_url: ""  # Empty for localhost
  listen_port: 25565
  port_range_min: 25566
  port_range_max: 25666
```

## Setup Steps

### 1. Start DiscoPanel

```bash
./discopanel -config config.localhost.yaml
```

### 2. Create Test Servers

1. Open DiscoPanel UI at http://localhost:8080
2. Create multiple test servers (e.g., "survival", "creative", "modded")
3. Make sure they use different internal ports (auto-assigned)

### 3. Configure Hostnames

For each server, go to the "Routing" tab and set custom hostnames:
- Server 1: `survival.local`
- Server 2: `creative.local`
- Server 3: `modded.local`

### 4. Update Your Hosts File

Add these entries to your system's hosts file:

**Linux/Mac** (`/etc/hosts`):
```
127.0.0.1 survival.local
127.0.0.1 creative.local
127.0.0.1 modded.local
```

**Windows** (`C:\Windows\System32\drivers\etc\hosts`):
```
127.0.0.1 survival.local
127.0.0.1 creative.local
127.0.0.1 modded.local
```

### 5. Start Your Servers

Start the Minecraft servers through the DiscoPanel UI.

### 6. Connect with Minecraft

In Minecraft, add servers with these addresses:
- `survival.local` (no port needed if using 25565)
- `creative.local`
- `modded.local`

The proxy will route each connection to the correct server based on the hostname!

## Alternative Testing Methods

### Method 1: Using Different Ports

If you can't modify hosts file, you can test by:
1. Leaving `base_url` empty
2. Setting different custom hostnames like `server1.test`, `server2.test`
3. Connecting with full hostname:port (e.g., `server1.test:25565`)

### Method 2: Using Subdomains of localhost

Some systems support subdomains of localhost:
- `survival.localhost:25565`
- `creative.localhost:25565`

### Method 3: Using Local DNS

Use a local DNS server like dnsmasq or Pi-hole to resolve custom domains.

## Debugging

### Check Proxy Status

```bash
curl http://localhost:8080/api/v1/proxy/status
```

### View Active Routes

```bash
curl http://localhost:8080/api/v1/proxy/routes
```

### Common Issues

1. **"Connection Refused"**
   - Ensure the proxy is running (check logs)
   - Verify the server is started
   - Check firewall settings

2. **"No Route Found"**
   - Verify hostname is configured correctly
   - Ensure server is running
   - Check that hosts file entries are correct

3. **"Can't Resolve Hostname"**
   - Hosts file not updated correctly
   - DNS cache needs flushing (`sudo dscacheutil -flushcache` on Mac)

## Testing Multiple Clients

To test with multiple Minecraft clients connecting to different servers:

1. Use different Minecraft profiles/instances
2. Connect each to a different hostname
3. Verify each connects to the correct server

## Logs

Check DiscoPanel logs for proxy routing information:
```
Proxying connection client=127.0.0.1:xxxxx hostname=survival.local backend=localhost:25001
```