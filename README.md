# DiscoPanel

A modern Minecraft server hosting panel built with Go, designed specifically for managing modded Minecraft servers with support for Forge, Fabric, NeoForge, and other mod loaders.

## Features

- **Docker-based Server Management**: Each Minecraft server runs in its own isolated Docker container
- **Multiple Mod Loader Support**: Forge, Fabric, NeoForge, Paper, Spigot, and Vanilla
- **Modern SvelteKit UI**: Built with Svelte 5 and Bootstrap for a responsive, reactive interface
- **REST API**: Full API for programmatic access
- **Mod Management**: Upload, enable/disable, and manage mods per server
- **File Management**: Browse and edit server files directly
- **Server Configuration**: Edit server.properties through the UI
- **Real-time Logs**: View server logs in real-time
- **Resource Management**: Set memory limits and monitor resource usage

## Prerequisites

- Go 1.24+
- Node.js 18+ (for SvelteKit frontend)
- Docker
- SQLite (via GORM)

## Installation

1. Clone the repository:
```bash
git clone https://github.com/nickheyer/discopanel.git
cd discopanel
```

2. Install Go dependencies:
```bash
go mod download
```

3. Install and build the frontend:
```bash
cd web
npm install
npm run build
cd ..
```

4. Build the application:
```bash
go build -o discopanel ./cmd/discopanel
```

## Usage

Start the DiscoPanel server:

```bash
./discopanel
```

Options:
- `-port`: HTTP server port (default: 8080)
- `-db`: Database file path (default: ./discopanel.db)
- `-data`: Data directory for server files (default: ./data)
- `-docker`: Docker daemon host (default: unix:///var/run/docker.sock)

Access the web UI at `http://localhost:8080`

## API Documentation

### Servers

- `GET /api/v1/servers` - List all servers
- `POST /api/v1/servers` - Create a new server
- `GET /api/v1/servers/{id}` - Get server details
- `PUT /api/v1/servers/{id}` - Update server
- `DELETE /api/v1/servers/{id}` - Delete server
- `POST /api/v1/servers/{id}/start` - Start server
- `POST /api/v1/servers/{id}/stop` - Stop server
- `POST /api/v1/servers/{id}/restart` - Restart server
- `GET /api/v1/servers/{id}/logs` - Get server logs

### Server Configuration

- `GET /api/v1/servers/{id}/config` - Get server configuration
- `PUT /api/v1/servers/{id}/config` - Update server configuration

### Mods

- `GET /api/v1/servers/{id}/mods` - List server mods
- `POST /api/v1/servers/{id}/mods` - Upload a mod
- `GET /api/v1/servers/{id}/mods/{modId}` - Get mod details
- `PUT /api/v1/servers/{id}/mods/{modId}` - Update mod
- `DELETE /api/v1/servers/{id}/mods/{modId}` - Delete mod

### Files

- `GET /api/v1/servers/{id}/files` - List files in directory
- `POST /api/v1/servers/{id}/files` - Upload file
- `GET /api/v1/servers/{id}/files/{path}` - Download file
- `PUT /api/v1/servers/{id}/files/{path}` - Update file
- `DELETE /api/v1/servers/{id}/files/{path}` - Delete file

## Architecture

DiscoPanel uses a clean architecture with:

- **Frontend**: SvelteKit with Svelte 5 and Bootstrap for the UI
- **Backend**: Go REST API server
- **Database**: SQLite with GORM ORM
- **Routing**: Gorilla Mux for HTTP routing
- **Containers**: Docker SDK for container management
- **Minecraft**: itzg/minecraft-server Docker images

## Development

### Running in Development Mode

For development, you can run the frontend and backend separately:

1. Start the Go backend:
```bash
go run ./cmd/discopanel
```

2. In another terminal, start the SvelteKit dev server:
```bash
cd web
npm run dev
```

The SvelteKit dev server will proxy API requests to the Go backend (configure in `vite.config.js`).

### Building for Production

1. Build the frontend:
```bash
cd web
npm run build
```

2. Build the Go binary:
```bash
go build -o discopanel ./cmd/discopanel
```

The project structure:

```
discopanel/
├── cmd/discopanel/       # Main application entry point
├── internal/
│   ├── api/             # HTTP API handlers
│   ├── docker/          # Docker client and container management
│   ├── minecraft/       # Minecraft-specific logic
│   ├── models/          # Data models
│   ├── proxy/           # Minecraft proxy implementation
│   └── storage/         # Database storage layer (GORM)
├── pkg/
│   ├── logger/          # Logging utilities
│   └── utils/           # Shared utilities
├── web/                 # SvelteKit frontend application
│   ├── src/            # Source files
│   ├── static/         # Static assets
│   └── build/          # Build output (served by Go)
└── deployments/         # Deployment configurations
```

## Docker Image

DiscoPanel uses the `itzg/minecraft-server` Docker image which provides excellent support for various Minecraft server types and mod loaders.

## Future Features

- Minecraft server reverse proxy for single-port access
- Automated backups
- Player management
- Plugin/mod auto-updates
- Server templates
- Multi-user support with permissions

## License

MIT License

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
