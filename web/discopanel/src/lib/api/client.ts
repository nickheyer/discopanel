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

function encodeFilePath(path: string): string {
  return path.split('/').map(segment => encodeURIComponent(segment)).join('/');
}

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

  async clearServerLogs(id: string): Promise<void> {
    this.request<ServerLogsResponse>(`/servers/${id}/logs`, { skipLoading: true });
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
    if (path && path.length > 0) params.append('path', encodeFilePath(path));
    if (tree) params.append('tree', 'true');
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.request<FileInfo[]>(`/servers/${serverId}/files${query}`);
  }

  async uploadFile(serverId: string, file: File, path: string = ''): Promise<UploadResponse> {
    const formData = new FormData();
    formData.append('file', file);
    if (path && path.length > 0) formData.append('path', encodeFilePath(path));

    return this.request<UploadResponse>(`/servers/${serverId}/files`, {
      method: 'POST',
      body: formData,
    });
  }

  async downloadFile(serverId: string, path: string): Promise<Blob> {
    return this.requestBlob(`/servers/${serverId}/files/${encodeFilePath(path)}`);
  }

  async updateFile(serverId: string, path: string, content: string): Promise<UploadResponse> {
    return this.request<UploadResponse>(`/servers/${serverId}/files/${encodeFilePath(path)}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'text/plain',
      },
      body: content,
    });
  }

  async deleteFile(serverId: string, path: string): Promise<void> {
    const encodedPath = encodeFilePath(path);
    const response = await fetch(`${API_BASE}/servers/${serverId}/files/${encodedPath}`, {
      method: 'DELETE',
      headers: authStore.getHeaders(),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Failed to delete file');
    }
  }

  async renameFile(serverId: string, path: string, newName: string): Promise<void> {
    const encodedPath = encodeFilePath(path);
    const response = await fetch(`${API_BASE}/servers/${serverId}/rename/${encodedPath}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...authStore.getHeaders(),
      },
      body: JSON.stringify({ new_name: newName }),
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Failed to rename file');
    }
  }

  async extractArchive(serverId: string, path: string): Promise<{ message: string; archive_path: string; extraction_path: string }> {
    return this.request<{ message: string; archive_path: string; extraction_path: string }>(`/servers/${serverId}/extract/${encodeFilePath(path)}`, {
      method: 'POST',
    });
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

  async deleteModpack(id: string): Promise<{ message: string; id: string }> {
    return this.request<{ message: string; id: string }>(`/modpacks/${id}`, {
      method: 'DELETE',
    });
  }

  // Support helpers
  async generateSupportBundle(uploadToSupport: boolean = false): Promise<{
    success: boolean;
    message: string;
    bundle_path?: string;
    reference_id?: string;
  }> {
    const url = uploadToSupport ? '/support/bundle?upload=true' : '/support/bundle';
    return this.request<{
      success: boolean;
      message: string;
      bundle_path?: string;
      reference_id?: string;
    }>(url, {
      method: 'POST',
    });
  }

  async downloadSupportBundle(path: string): Promise<void> {
    const response = await fetch(`${API_BASE}/support/bundle/download?path=${encodeURIComponent(path)}`, {
      headers: authStore.getHeaders(),
    });

    if (!response.ok) {
      throw new Error(`Failed to download support bundle: ${response.statusText}`);
    }

    // Trigger browser download
    const blob = await response.blob();
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = path.split('/').pop() || 'support-bundle.tar.gz';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(url);
  }

  // Console autocompletion
  async getCommandSuggestions(serverId: string, prefix: string): Promise<string[]> {
    return this.request<string[]>(`/servers/${serverId}/commands/suggest?prefix=${encodeURIComponent(prefix)}`);
  }

  async getPlayerSuggestions(serverId: string): Promise<string[]> {
    return this.request<string[]>(`/servers/${serverId}/players/suggest`);
  }

  // Mod updates
  async updateModFromRemote(serverId: string, modId: string, indexer?: string): Promise<Mod> {
    const params = indexer ? `?indexer=${encodeURIComponent(indexer)}` : '';
    return this.request<Mod>(`/servers/${serverId}/mods/${modId}/update${params}`, {
      method: 'POST',
    });
  }
}

export const api = new ApiClient();
