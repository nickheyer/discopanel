import type {
  Server,
  CreateServerRequest,
  UpdateServerRequest,
  Mod,
  UpdateModRequest,
  FileInfo,
  MinecraftVersion,
  ModLoaderInfo,
  DockerImageInfo,
  ServerLogsResponse,
  ServerStatusResponse,
  UploadResponse,
  ServerProperties,
  ApiError,
  ConfigCategory
} from './types';

const API_BASE = '/api/v1';

class ApiClient {
  private async request<T>(
    path: string,
    options: RequestInit = {}
  ): Promise<T> {
    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers: {
        ...options.headers,
      },
    });

    if (!response.ok) {
      const error: ApiError = await response.json();
      throw new Error(error.error || `HTTP error! status: ${response.status}`);
    }

    return response.json();
  }

  private async requestBlob(
    path: string,
    options: RequestInit = {}
  ): Promise<Blob> {
    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers: {
        ...options.headers,
      },
    });

    if (!response.ok) {
      const error: ApiError = await response.json();
      throw new Error(error.error || `HTTP error! status: ${response.status}`);
    }

    return response.blob();
  }

  // Minecraft Information
  async getMinecraftVersions(): Promise<MinecraftVersion> {
    return this.request<MinecraftVersion>('/minecraft/versions');
  }

  async getModLoaders(): Promise<{ modloaders: ModLoaderInfo[] }> {
    return this.request<{ modloaders: ModLoaderInfo[] }>('/minecraft/modloaders');
  }

  async getDockerImages(): Promise<{ images: DockerImageInfo[] }> {
    return this.request<{ images: DockerImageInfo[] }>('/minecraft/docker-images');
  }

  // Server Management
  async getServers(): Promise<Server[]> {
    return this.request<Server[]>('/servers');
  }

  async getServer(id: string): Promise<Server> {
    return this.request<Server>(`/servers/${id}`);
  }

  async createServer(data: CreateServerRequest): Promise<Server> {
    return this.request<Server>('/servers', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    });
  }

  async updateServer(id: string, data: UpdateServerRequest): Promise<Server> {
    return this.request<Server>(`/servers/${id}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    });
  }

  async deleteServer(id: string): Promise<void> {
    await fetch(`${API_BASE}/servers/${id}`, {
      method: 'DELETE',
    });
  }

  // Server Control
  async startServer(id: string): Promise<ServerStatusResponse> {
    return this.request<ServerStatusResponse>(`/servers/${id}/start`, {
      method: 'POST',
    });
  }

  async stopServer(id: string): Promise<ServerStatusResponse> {
    return this.request<ServerStatusResponse>(`/servers/${id}/stop`, {
      method: 'POST',
    });
  }

  async restartServer(id: string): Promise<ServerStatusResponse> {
    return this.request<ServerStatusResponse>(`/servers/${id}/restart`, {
      method: 'POST',
    });
  }

  async getServerLogs(id: string, tail: number = 100): Promise<ServerLogsResponse> {
    return this.request<ServerLogsResponse>(`/servers/${id}/logs?tail=${tail}`);
  }

  async sendServerCommand(id: string, command: string): Promise<{ success: boolean; output?: string; error?: string }> {
    return this.request<{ success: boolean; output?: string; error?: string }>(`/servers/${id}/command`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ command }),
    });
  }

  // Server Configuration
  async getServerConfig(id: string): Promise<ConfigCategory[]> {
    return this.request<ConfigCategory[]>(`/servers/${id}/config`);
  }

  async updateServerConfig(id: string, properties: Record<string, any>): Promise<ConfigCategory[]> {
    return this.request<ConfigCategory[]>(`/servers/${id}/config`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(properties),
    });
  }

  // Mod Management
  async getMods(serverId: string): Promise<Mod[]> {
    return this.request<Mod[]>(`/servers/${serverId}/mods`);
  }

  async getMod(serverId: string, modId: string): Promise<Mod> {
    return this.request<Mod>(`/servers/${serverId}/mods/${modId}`);
  }

  async uploadMod(serverId: string, file: File, metadata?: {
    name?: string;
    version?: string;
    mod_id?: string;
    description?: string;
  }): Promise<Mod> {
    const formData = new FormData();
    formData.append('mod', file);
    
    if (metadata?.name) formData.append('name', metadata.name);
    if (metadata?.version) formData.append('version', metadata.version);
    if (metadata?.mod_id) formData.append('mod_id', metadata.mod_id);
    if (metadata?.description) formData.append('description', metadata.description);

    return this.request<Mod>(`/servers/${serverId}/mods`, {
      method: 'POST',
      body: formData,
    });
  }

  async updateMod(serverId: string, modId: string, data: UpdateModRequest): Promise<Mod> {
    return this.request<Mod>(`/servers/${serverId}/mods/${modId}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    });
  }

  async deleteMod(serverId: string, modId: string): Promise<void> {
    await fetch(`${API_BASE}/servers/${serverId}/mods/${modId}`, {
      method: 'DELETE',
    });
  }

  // File Management
  async listFiles(serverId: string, path: string = '', tree: boolean = false): Promise<FileInfo[]> {
    const params = new URLSearchParams();
    if (path) params.append('path', path);
    if (tree) params.append('tree', 'true');
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<FileInfo[]>(`/servers/${serverId}/files${query}`);
  }

  async uploadFile(serverId: string, file: File, path: string = ''): Promise<UploadResponse> {
    const formData = new FormData();
    formData.append('file', file);
    if (path) formData.append('path', path);

    return this.request<UploadResponse>(`/servers/${serverId}/files`, {
      method: 'POST',
      body: formData,
    });
  }

  async downloadFile(serverId: string, path: string): Promise<Blob> {
    return this.requestBlob(`/servers/${serverId}/files/${path}`);
  }

  async updateFile(serverId: string, path: string, content: string): Promise<UploadResponse> {
    return this.request<UploadResponse>(`/servers/${serverId}/files/${path}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'text/plain',
      },
      body: content,
    });
  }

  async deleteFile(serverId: string, path: string): Promise<void> {
    await fetch(`${API_BASE}/servers/${serverId}/files/${path}`, {
      method: 'DELETE',
    });
  }
}

export const api = new ApiClient();