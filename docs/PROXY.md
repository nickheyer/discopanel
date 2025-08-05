# DiscoPanel Minecraft Proxy

DiscoPanel includes a built-in TCP proxy that allows multiple Minecraft servers to be accessed through a single IP address and port using different hostnames.

## How It Works

The proxy intercepts the initial Minecraft handshake packet, reads the hostname the player used to connect, and routes the connection to the appropriate backend server based on that hostname.

## Features

- **Single Port Access**: All servers accessible through one port (e.g., 25565 or 443)
- **Hostname-Based Routing**: Route players based on the domain they connect with
- **Automatic Route Management**: Routes are automatically created/removed when servers start/stop
- **Docker Integration**: Works seamlessly with Docker-containerized servers
- **No Client Modifications**: Works with vanilla Minecraft clients

## Configuration

Enable the proxy in your `config.yaml`:

```yaml
proxy:
  enabled: true
  base_url: "mc.example.com"
  listen_port: 25565
  port_range_min: 25565
  port_range_max: 25665
```

### Configuration Options

- `enabled`: Enable/disable the proxy feature
- `base_url`: Your domain for Minecraft servers (e.g., "mc.example.com")
- `listen_port`: Port the proxy listens on (default: 25565)
- `port_range_min/max`: Range for allocating internal proxy ports to servers

## DNS Setup

You need to configure DNS to point to your DiscoPanel server:

### Option 1: Wildcard DNS (Recommended)
```
*.mc.example.com → Your_Server_IP
```

### Option 2: Individual Records
```
survival.mc.example.com → Your_Server_IP
creative.mc.example.com → Your_Server_IP
modded.mc.example.com → Your_Server_IP
```

## Usage

Once configured, players can connect to servers using:

- `servername.mc.example.com:25565`
- Or just `servername.mc.example.com` (if using default port 25565)

The proxy will automatically route them to the correct server based on the hostname.

## How Routing Works

1. When a server is created, it gets allocated a unique proxy port
2. When the server starts, a route is created: `hostname → backend_server:port`
3. When a player connects to `survival.mc.example.com`, the proxy:
   - Reads the hostname from the Minecraft handshake
   - Finds the matching route
   - Forwards the connection to the backend server
4. When the server stops, the route is removed

## API Endpoints

### Get Proxy Status
```
GET /api/v1/proxy/status
```

Response:
```json
{
  "enabled": true,
  "running": true,
  "listen_port": 25565,
  "base_url": "mc.example.com",
  "active_routes": 3
}
```

### Get Active Routes
```
GET /api/v1/proxy/routes
```

Response:
```json
[
  {
    "server_id": "abc123",
    "hostname": "survival.mc.example.com",
    "backend_host": "localhost",
    "backend_port": 25001,
    "active": true
  }
]
```

## Advanced Usage

### Using Port 443

You can run the proxy on port 443 to bypass restrictive firewalls:

```yaml
proxy:
  listen_port: 443
```

Players can then connect without specifying a port:
- `survival.mc.example.com`

### Custom Domains per Server

While the system uses `servername.base_url` by default, you can override this by modifying the proxy manager's `generateHostname` function to support custom domains per server.

## Troubleshooting

### Players Can't Connect

1. Check DNS is configured correctly: `nslookup servername.mc.example.com`
2. Verify proxy is running: Check `/api/v1/proxy/status`
3. Ensure server is running and has an active route: Check `/api/v1/proxy/routes`
4. Check firewall allows connections on the proxy port

### "No Route Found" Errors

- Ensure the server is running
- Verify the hostname matches exactly (case-insensitive)
- Check the server has a proxy port allocated

### Performance Considerations

- The proxy adds minimal latency (typically <1ms)
- Each connection uses minimal memory (~4KB)
- Can handle thousands of concurrent connections

## Security Notes

- The proxy does not perform authentication - use server whitelists
- Consider rate limiting at the firewall level
- The proxy only forwards Minecraft protocol traffic