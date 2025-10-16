<!-- A reusable server widget component -->
<script lang="ts">
  import type { ServerType } from '$lib/api/types';

  export let name: string;
  export let type: ServerType;
  export let version: string;
  export let status: 'online' | 'offline' = 'offline';

  const statusColors = {
    online: 'bg-green-500',
    offline: 'bg-red-500'
  };
</script>

<div class="rounded-lg bg-gray-800 p-4 shadow-lg hover:shadow-xl transition-shadow">
  <div class="flex items-center justify-between mb-4">
    <h3 class="text-lg font-semibold text-white">{name}</h3>
    <div class="flex items-center space-x-2">
      <span class="{statusColors[status]} w-3 h-3 rounded-full"></span>
      <span class="text-gray-300 text-sm">{status}</span>
    </div>
  </div>
  <div class="text-gray-400 text-sm space-y-2">
    <div>Type: {type}</div>
    <div>Version: {version}</div>
  </div>
  <div class="mt-4 flex space-x-2">
    <button class="px-3 py-1 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded">
      Manage
    </button>
    {#if status === 'offline'}
      <button class="px-3 py-1 text-sm bg-green-600 hover:bg-green-700 text-white rounded">
        Start
      </button>
    {:else}
      <button class="px-3 py-1 text-sm bg-red-600 hover:bg-red-700 text-white rounded">
        Stop
      </button>
    {/if}
  </div>
</div>