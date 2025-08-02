// API utility functions for DiscoPanel

const API_BASE = '/api/v1';

export interface Server {
    id: string;
    name: string;
    description: string;
    mod_loader: string;
    mc_version: string;
    container_id: string;
    status: 'stopped' | 'starting' | 'running' | 'stopping' | 'error';
    port: number;
    proxy_port: number;
    max_players: number;
    memory: number;
    created_at: string;
    updated_at: string;
    last_started: string | null;
    java_version: string;
    data_path: string;
}

export interface ServerConfig {
    id: string;
    server_id: string;
    difficulty: string;
    gamemode: string;
    level_name: string;
    level_seed: string;
    max_players: number;
    view_distance: number;
    online_mode: boolean;
    pvp: boolean;
    allow_nether: boolean;
    allow_flight: boolean;
    spawn_animals: boolean;
    spawn_monsters: boolean;
    spawn_npcs: boolean;
    generate_structures: boolean;
    motd: string;
    updated_at: string;
}

export interface Mod {
    id: string;
    server_id: string;
    name: string;
    file_name: string;
    version: string;
    mod_id: string;
    description: string;
    enabled: boolean;
    uploaded_at: string;
    file_size: number;
}

class ApiClient {
    async request<T>(path: string, options: RequestInit = {}): Promise<T> {
        const response = await fetch(`${API_BASE}${path}`, {
            ...options,
            headers: {
                'Content-Type': 'application/json',
                ...options.headers
            }
        });

        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: 'Request failed' }));
            throw new Error(error.error || `HTTP ${response.status}`);
        }

        if (response.status === 204) {
            return {} as T;
        }

        return response.json();
    }

    // Server operations
    async listServers(): Promise<Server[]> {
        return this.request<Server[]>('/servers');
    }

    async getServer(id: string): Promise<Server> {
        return this.request<Server>(`/servers/${id}`);
    }

    async createServer(server: Partial<Server>): Promise<Server> {
        return this.request<Server>('/servers', {
            method: 'POST',
            body: JSON.stringify(server)
        });
    }

    async updateServer(id: string, updates: Partial<Server>): Promise<Server> {
        return this.request<Server>(`/servers/${id}`, {
            method: 'PUT',
            body: JSON.stringify(updates)
        });
    }

    async deleteServer(id: string): Promise<void> {
        await this.request<void>(`/servers/${id}`, { method: 'DELETE' });
    }

    async startServer(id: string): Promise<{ status: string }> {
        return this.request<{ status: string }>(`/servers/${id}/start`, { method: 'POST' });
    }

    async stopServer(id: string): Promise<{ status: string }> {
        return this.request<{ status: string }>(`/servers/${id}/stop`, { method: 'POST' });
    }

    async restartServer(id: string): Promise<{ status: string }> {
        return this.request<{ status: string }>(`/servers/${id}/restart`, { method: 'POST' });
    }

    async getServerLogs(id: string, tail = 100): Promise<{ logs: string }> {
        return this.request<{ logs: string }>(`/servers/${id}/logs?tail=${tail}`);
    }

    // Server config operations
    async getServerConfig(id: string): Promise<ServerConfig> {
        return this.request<ServerConfig>(`/servers/${id}/config`);
    }

    async updateServerConfig(id: string, config: Partial<ServerConfig>): Promise<ServerConfig> {
        return this.request<ServerConfig>(`/servers/${id}/config`, {
            method: 'PUT',
            body: JSON.stringify(config)
        });
    }

    // Mod operations
    async listMods(serverId: string): Promise<Mod[]> {
        return this.request<Mod[]>(`/servers/${serverId}/mods`);
    }

    async uploadMod(serverId: string, file: File, metadata: Partial<Mod>): Promise<Mod> {
        const formData = new FormData();
        formData.append('mod', file);
        Object.entries(metadata).forEach(([key, value]) => {
            if (value !== undefined) {
                formData.append(key, String(value));
            }
        });

        const response = await fetch(`${API_BASE}/servers/${serverId}/mods`, {
            method: 'POST',
            body: formData
        });

        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: 'Upload failed' }));
            throw new Error(error.error || `HTTP ${response.status}`);
        }

        return response.json();
    }

    async updateMod(serverId: string, modId: string, updates: Partial<Mod>): Promise<Mod> {
        return this.request<Mod>(`/servers/${serverId}/mods/${modId}`, {
            method: 'PUT',
            body: JSON.stringify(updates)
        });
    }

    async deleteMod(serverId: string, modId: string): Promise<void> {
        await this.request<void>(`/servers/${serverId}/mods/${modId}`, { method: 'DELETE' });
    }
}

export const api = new ApiClient();