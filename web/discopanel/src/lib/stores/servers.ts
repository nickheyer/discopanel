import { writable, derived } from 'svelte/store';
import type { Server } from '$lib/api/types';
import { api } from '$lib/api/client';

function createServersStore() {
  const { subscribe, set, update } = writable<Server[]>([]);

  return {
    subscribe,
    fetchServers: async () => {
      try {
        const servers = await api.getServers();
        set(servers);
        return servers;
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
  $servers => $servers.filter(server => server.status === 'running')
);

export const stoppedServers = derived(
  serversStore,
  $servers => $servers.filter(server => server.status === 'stopped')
);