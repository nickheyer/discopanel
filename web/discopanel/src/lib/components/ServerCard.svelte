<script lang="ts">
    import type { Server } from '$lib/api';
    import { api } from '$lib/api';
    
    let { server = $bindable() }: { server: Server } = $props();
    
    let isLoading = $state(false);
    
    function getStatusColor(status: string) {
        switch (status) {
            case 'running': return 'text-green-600 bg-green-50 border-green-200';
            case 'stopped': return 'text-gray-600 bg-gray-50 border-gray-200';
            case 'starting': return 'text-blue-600 bg-blue-50 border-blue-200';
            case 'stopping': return 'text-yellow-600 bg-yellow-50 border-yellow-200';
            case 'error': return 'text-red-600 bg-red-50 border-red-200';
            default: return 'text-gray-600 bg-gray-50 border-gray-200';
        }
    }
    
    async function handleStart() {
        isLoading = true;
        try {
            await api.startServer(server.id);
            server.status = 'starting';
        } catch (err) {
            console.error('Failed to start server:', err);
        } finally {
            isLoading = false;
        }
    }
    
    async function handleStop() {
        isLoading = true;
        try {
            await api.stopServer(server.id);
            server.status = 'stopping';
        } catch (err) {
            console.error('Failed to stop server:', err);
        } finally {
            isLoading = false;
        }
    }
</script>

<div class="bg-white rounded-lg shadow-sm border border-gray-200 p-6 hover:shadow-md transition-shadow">
    <div class="flex justify-between items-start mb-4">
        <div>
            <h3 class="text-lg font-semibold text-gray-900">{server.name}</h3>
            {#if server.description}
                <p class="text-sm text-gray-600 mt-1">{server.description}</p>
            {/if}
        </div>
        <span class="px-3 py-1 text-xs font-medium rounded-full border {getStatusColor(server.status)}">
            {server.status}
        </span>
    </div>
    
    <div class="grid grid-cols-2 gap-3 text-sm mb-4">
        <div>
            <span class="text-gray-500">Version:</span>
            <span class="ml-2 font-medium">{server.mc_version}</span>
        </div>
        <div>
            <span class="text-gray-500">Mod Loader:</span>
            <span class="ml-2 font-medium">{server.mod_loader}</span>
        </div>
        <div>
            <span class="text-gray-500">Port:</span>
            <span class="ml-2 font-medium">{server.port}</span>
        </div>
        <div>
            <span class="text-gray-500">Memory:</span>
            <span class="ml-2 font-medium">{server.memory} MB</span>
        </div>
    </div>
    
    <div class="flex gap-2">
        {#if server.status === 'stopped'}
            <button
                onclick={handleStart}
                disabled={isLoading}
                class="px-4 py-2 bg-green-600 text-white text-sm font-medium rounded-md hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
                Start
            </button>
        {:else if server.status === 'running'}
            <button
                onclick={handleStop}
                disabled={isLoading}
                class="px-4 py-2 bg-red-600 text-white text-sm font-medium rounded-md hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
                Stop
            </button>
        {/if}
        
        <a
            href="/servers/{server.id}"
            class="px-4 py-2 bg-gray-100 text-gray-700 text-sm font-medium rounded-md hover:bg-gray-200"
        >
            Manage
        </a>
    </div>
</div>