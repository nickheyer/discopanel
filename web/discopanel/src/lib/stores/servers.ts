import { writable, derived } from 'svelte/store';
import type { Server } from '$lib/proto/discopanel/v1/common_pb';
import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
import { create } from '@bufbuild/protobuf';
import { ListServersRequestSchema } from '$lib/proto/discopanel/v1/server_pb';

function createServersStore() {
  const { subscribe, set, update } = writable<Server[]>([]);

  return {
    subscribe,
    set,
    fetchServers: async (skipLoading = false) => {
      try {
        const request = create(ListServersRequestSchema, { fullStats: false });
        const callOptions = skipLoading ? silentCallOptions : undefined;
        const response = await rpcClient.server.listServers(request, callOptions);
        set(response.servers);
        return response.servers;
      } catch (error) {
        console.error('Failed to fetch servers:', error);
        throw error;
      }
    },
    updateServer: (server: Server) => {
      update(servers => {
        const index = servers.findIndex(s => s.id === server.id);
        if (index !== -1) {
          servers[index] = server;
        }
        return servers;
      });
    },
    removeServer: (id: string) => {
      update(servers => servers.filter(s => s.id !== id));
    },
    addServer: (server: Server) => {
      update(servers => [...servers, server]);
    }
  };
}

export const serversStore = createServersStore();

export const runningServers = derived(
  serversStore,
  $servers => $servers.filter(server => server.status === ServerStatus.RUNNING)
);

export const stoppedServers = derived(
  serversStore,
  $servers => $servers.filter(server => server.status === ServerStatus.STOPPED)
);