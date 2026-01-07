import { createClient, type Client, type Interceptor } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { authStore } from '$lib/stores/auth';
import { toast } from 'svelte-sonner';
import { loadingStore } from '$lib/stores/loading.svelte';

// SERVICES
import { AuthService } from '$lib/proto/discopanel/v1/auth_pb';
import { ConfigService } from '$lib/proto/discopanel/v1/config_pb';
import { FileService } from '$lib/proto/discopanel/v1/file_pb';
import { MinecraftService } from '$lib/proto/discopanel/v1/minecraft_pb';
import { ModService } from '$lib/proto/discopanel/v1/mod_pb';
import { ModpackService } from '$lib/proto/discopanel/v1/modpack_pb';
import { ProxyService } from '$lib/proto/discopanel/v1/proxy_pb';
import { ServerService } from '$lib/proto/discopanel/v1/server_pb';
import { SupportService } from '$lib/proto/discopanel/v1/support_pb';
import { TaskService } from '$lib/proto/discopanel/v1/task_pb';
import { UserService } from '$lib/proto/discopanel/v1/user_pb';

// Login auth interception
const authInterceptor: Interceptor = (next) => async (req) => {
  // Auth headers
  const authHeaders = authStore.getHeaders();
  Object.entries(authHeaders).forEach(([key, value]) => {
    req.header.set(key, value as string);
  });

  // Operation ID for loading tracking
  const operationId = `rpc-${req.service.typeName}-${req.method.name}-${Date.now()}`;

  // Show loading indicator
  const showLoading = !req.method.name.toLowerCase().includes('status');
  if (showLoading) {
    loadingStore.start(operationId);
  }

  try {
    const res = await next(req);
    return res;
  } catch (error: any) {
    // Show error toast
    const message = error.message || 'An error occurred';
    toast.error(message);
    throw error;
  } finally {
    if (showLoading) {
      loadingStore.stop(operationId);
    }
  }
};

// Transport w/ auth
const transport = createConnectTransport({
  baseUrl: "",
  interceptors: [authInterceptor]
});

// Clients for each service
export class RpcClient {
  public readonly auth: Client<typeof AuthService>;
  public readonly config: Client<typeof ConfigService>;
  public readonly file: Client<typeof FileService>;
  public readonly minecraft: Client<typeof MinecraftService>;
  public readonly mod: Client<typeof ModService>;
  public readonly modpack: Client<typeof ModpackService>;
  public readonly proxy: Client<typeof ProxyService>;
  public readonly server: Client<typeof ServerService>;
  public readonly support: Client<typeof SupportService>;
  public readonly task: Client<typeof TaskService>;
  public readonly user: Client<typeof UserService>;

  constructor() {
    this.auth = createClient(AuthService, transport);
    this.config = createClient(ConfigService, transport);
    this.file = createClient(FileService, transport);
    this.minecraft = createClient(MinecraftService, transport);
    this.mod = createClient(ModService, transport);
    this.modpack = createClient(ModpackService, transport);
    this.proxy = createClient(ProxyService, transport);
    this.server = createClient(ServerService, transport);
    this.support = createClient(SupportService, transport);
    this.task = createClient(TaskService, transport);
    this.user = createClient(UserService, transport);
  }
}

// singleton
export const rpcClient = new RpcClient();