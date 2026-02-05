import { createClient, type Client, type Interceptor, type CallOptions } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { authStore } from '$lib/stores/auth';
import { toast } from 'svelte-sonner';
import { loadingStore } from '$lib/stores/loading.svelte';
import { wsClient } from './ws-client';
import type { DescMessage, MessageShape } from '@bufbuild/protobuf';
import { nanoid } from 'nanoid';

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
import { UploadService } from '$lib/proto/discopanel/v1/upload_pb';
import { UserService } from '$lib/proto/discopanel/v1/user_pb';
import { ModuleService } from '$lib/proto/discopanel/v1/module_pb';

// Header to mark requests as silent / no loader
const SILENT_HEADER = 'X-Silent-Request';

export const silentCallOptions = { headers: new Headers({ [SILENT_HEADER]: 'true' }) };

// Pending topic captures: captureId → resolve function
const pendingTopicCaptures = new Map<string, (topic: string) => void>();

// Login auth interception + X-Alive topic capture
const authInterceptor: Interceptor = (next) => async (req) => {
  // Auth headers
  const authHeaders = authStore.getHeaders();
  Object.entries(authHeaders).forEach(([key, value]) => {
    req.header.set(key, value as string);
  });

  // Check for silence
  const isSilent = req.header.get(SILENT_HEADER) === 'true';

  // Operation ID for loading tracking
  const operationId = `rpc-${req.service.typeName}-${req.method.name}-${Date.now()}`;

  // Show loading indicator
  const showLoading = !isSilent && !req.method.name.toLowerCase().includes('status');
  if (showLoading) {
    loadingStore.start(operationId);
  }

  try {
    const res = await next(req);

    // Capture X-Alive topic for keep-alive requests
    const captureId = req.header.get('X-Live-Id');
    const aliveTopic = res.header?.get('X-Alive');
    if (captureId && aliveTopic) {
      const resolve = pendingTopicCaptures.get(captureId);
      if (resolve) {
        resolve(aliveTopic);
        pendingTopicCaptures.delete(captureId);
      }
    }

    return res;
  } catch (error: any) {
    // Clean up pending capture on error
    const captureId = req.header.get('X-Live-Id');
    if (captureId) {
      pendingTopicCaptures.delete(captureId);
    }

    // Show error toast
    if (!isSilent) {
      const message = error.rawMessage || error.message || 'An error occurred';
      toast.error(message);
    }
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
  public readonly upload: Client<typeof UploadService>;
  public readonly user: Client<typeof UserService>;
  public readonly module: Client<typeof ModuleService>;

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
    this.upload = createClient(UploadService, transport);
    this.user = createClient(UserService, transport);
    this.module = createClient(ModuleService, transport);
  }
}

// singleton
export const rpcClient = new RpcClient();

/**
 * Makes an RPC call with X-Keep-Alive, captures the X-Alive topic from the response,
 * and subscribes to that topic via WebSocket for real-time updates.
 *
 * @param rpcCall  - function that makes the RPC call, receives CallOptions
 * @param schema   - protobuf response schema for deserializing WS updates
 * @param onUpdate - callback invoked with each pushed update
 * @param key      - primary key value sent as X-Keep-Alive (e.g. server ID).
 *                   Omit or pass "true" for list/no-id endpoints.
 *
 * Returns the initial data and an unsubscribe function.
 */
export async function withLive<T extends DescMessage>(
  rpcCall: (opts?: CallOptions) => Promise<MessageShape<T>>,
  schema: T,
  onUpdate: (data: MessageShape<T>) => void,
  key?: string,
): Promise<{ data: MessageShape<T>; unsubscribe: () => void }> {
  const captureId = nanoid();

  // Register topic capture before making the call
  const topicPromise = new Promise<string>((resolve, reject) => {
    pendingTopicCaptures.set(captureId, resolve);
    // Timeout after 5s in case the header is never received
    setTimeout(() => {
      if (pendingTopicCaptures.has(captureId)) {
        pendingTopicCaptures.delete(captureId);
        reject(new Error('withLive: topic capture timed out'));
      }
    }, 10000);
  });

  // Make the RPC call with keep-alive headers
  // X-Keep-Alive carries the pk value so the server can build the topic
  const headers = new Headers({
    'X-Keep-Alive': key || 'true',
    'X-Silent-Request': 'true',
    'X-Live-Id': captureId,
  });

  const data = await rpcCall({ headers });

  // Wait for interceptor to capture the topic
  const topic = await topicPromise;
  const token = authStore.getToken()

  // Ensure WebSocket is connected
  wsClient.connect();

  // Subscribe via WebSocket
  wsClient.subscribe(topic, token, schema, onUpdate);

  return {
    data,
    unsubscribe: () => wsClient.unsubscribe(topic),
  };
}
