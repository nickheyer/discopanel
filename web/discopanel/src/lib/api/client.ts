import { toast } from 'svelte-sonner';
import { authStore } from '$lib/stores/auth';
import { loadingStore } from '$lib/stores/loading.svelte';
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
    options: RequestInit & { skipLoading?: boolean } = {}
  ): Promise<T> {
    // Generate unique operation ID for this request
    const operationId = `${options.method || 'GET'}-${path}-${Date.now()}`;
    
    // Don't show loading for polling operations or if explicitly skipped
    const showLoading = !options.skipLoading && !path.includes('?poll=true');
    
    if (showLoading) {
      loadingStore.start(operationId);
    }
    
    try {
      // Get auth headers
      const authHeaders = authStore.getHeaders();
      
      const response = await fetch(`${API_BASE}${path}`, {
        ...options,
        headers: {
          ...authHeaders,
          ...options.headers,
        },
      });

      if (!response.ok) {
        let errorMessage = `HTTP error! status: ${response.status}`;
        try {
          const error: ApiError = await response.json();
          errorMessage = error.error || errorMessage;
        } catch {
          // If response is not JSON, use default error message
        }
        
        // Show error toast
        toast.error(errorMessage);
        
        throw new Error(errorMessage);
      }

      return response.json();
    } finally {
      if (showLoading) {
        loadingStore.stop(operationId);
      }
    }
  }

  private async requestBlob(
    path: string,
    options: RequestInit = {}
  ): Promise<Blob> {
    // Get auth headers
    const authHeaders = authStore.getHeaders();
    
    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers: {
        ...authHeaders,
        ...options.headers,
      },
    });

    if (!response.ok) {
      let errorMessage = `HTTP error! status: ${response.status}`;
      try {
        const error: ApiError = await response.json();
        errorMessage = error.error || errorMessage;
      } catch {
        // If response is not JSON, use default error message
      }
      
      // Show error toast
      toast.error(errorMessage);
      
      throw new Error(errorMessage);
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
  async getServers(skipLoading = false): Promise<Server[]> {
    return this.request<Server[]>('/servers', { skipLoading });
  }

  async getServer(id: string, skipLoading = false): Promise<Server> {
    return this.request<Server>(`/servers/${id}`, { skipLoading });
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

  async getNextAvailablePort(): Promise<{ port: number; usedPorts: Record<number, boolean> }> {
    return this.request<{ port: number; usedPorts: Record<number, boolean> }>('/servers/next-port');
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
    return this.request<ServerLogsResponse>(`/servers/${id}/logs?tail=${tail}`, { skipLoading: true });
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

  async renameFile(serverId: string, path: string, newName: string): Promise<void> {
    const response = await fetch(`${API_BASE}/servers/${serverId}/rename/${path}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ new_name: newName }),
    });
    
    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Failed to rename file');
    }
  }

  // Proxy/Routing Management
  async getProxyStatus(): Promise<any> {
    return this.request<any>('/proxy/status');
  }

  async getProxyRoutes(): Promise<any[]> {
    return this.request<any[]>('/proxy/routes');
  }

  async updateProxyConfig(config: { enabled: boolean; base_url: string }): Promise<any> {
    return this.request<any>('/proxy/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
  }

  async getProxyListeners(): Promise<any[]> {
    return this.request<any[]>('/proxy/listeners');
  }

  async createProxyListener(listener: {
    port: number;
    name: string;
    description?: string;
    enabled?: boolean;
    is_default?: boolean;
  }): Promise<any> {
    return this.request<any>('/proxy/listeners', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(listener),
    });
  }

  async updateProxyListener(id: string, listener: {
    name?: string;
    description?: string;
    enabled?: boolean;
    is_default?: boolean;
  }): Promise<any> {
    return this.request<any>(`/proxy/listeners/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(listener),
    });
  }

  async deleteProxyListener(id: string): Promise<void> {
    await this.request<any>(`/proxy/listeners/${id}`, {
      method: 'DELETE',
    });
  }

  async getServerRouting(id: string): Promise<any> {
    return this.request<any>(`/servers/${id}/routing`);
  }

  async updateServerRouting(id: string, hostname: string): Promise<any> {
    return this.request<any>(`/servers/${id}/routing`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ proxy_hostname: hostname }),
    });
  }
}

export const api = new ApiClient();