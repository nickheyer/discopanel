<script lang="ts">
  import { onMount } from 'svelte';
  import type { ServerType } from '$lib/api/types';
  import ServerWidget from '$lib/components/widgets/server-widget.svelte';
  import { page } from '$app/stores';

  let servers: any[] = [];

  onMount(async () => {
    // TODO: Fetch servers from your API
    // For now, using mock data
    servers = [
      {
        id: '1',
        name: 'Survival Server',
        type: 'PAPER',
        version: '1.20.2',
        status: 'online'
      },
      {
        id: '2',
        name: 'Creative Server',
        type: 'SPIGOT',
        version: '1.19.4',
        status: 'offline'
      }
    ];
  });
</script>

<div class="container mx-auto p-4">
  <div class="flex justify-between items-center mb-6">
    <h1 class="text-2xl font-bold text-white">Server Dashboard</h1>
    <button 
      class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded"
      on:click={() => window.location.href = '/servers/new'}
    >
      Add Server
    </button>
  </div>

  <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
    {#each servers as server (server.id)}
      <ServerWidget {...server} />
    {/each}
  </div>
</div>