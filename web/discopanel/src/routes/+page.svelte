<script lang="ts">
    import { onMount } from 'svelte';
    import { api, type Server } from '$lib/api';
    import ServerCard from '$lib/components/ServerCard.svelte';
    import CreateServerModal from '$lib/components/CreateServerModal.svelte';
    
    let servers = $state<Server[]>([]);
    let loading = $state(true);
    let error = $state<string | null>(null);
    let showCreateModal = $state(false);
    
    async function loadServers() {
        loading = true;
        error = null;
        try {
            servers = await api.listServers();
        } catch (err) {
            error = err instanceof Error ? err.message : 'Failed to load servers';
        } finally {
            loading = false;
        }
    }
    
    onMount(() => {
        loadServers();
        // Refresh every 10 seconds
        const interval = setInterval(loadServers, 10000);
        return () => clearInterval(interval);
    });
</script>

<div class="min-h-screen bg-gray-50">
    <header class="bg-white shadow-sm border-b border-gray-200">
        <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
            <div class="flex justify-between items-center">
                <h1 class="text-2xl font-bold text-gray-900">DiscoPanel</h1>
                <button
                    onclick={() => showCreateModal = true}
                    class="px-4 py-2 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700"
                >
                    Create Server
                </button>
            </div>
        </div>
    </header>
    
    <main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {#if error}
            <div class="mb-6 p-4 bg-red-50 border border-red-200 rounded-md">
                <p class="text-red-700">{error}</p>
            </div>
        {/if}
        
        {#if loading}
            <div class="flex justify-center items-center h-64">
                <div class="text-gray-500">Loading servers...</div>
            </div>
        {:else if servers.length === 0}
            <div class="text-center py-12">
                <h3 class="text-lg font-medium text-gray-900 mb-2">No servers yet</h3>
                <p class="text-gray-500 mb-6">Get started by creating your first Minecraft server.</p>
                <button
                    onclick={() => showCreateModal = true}
                    class="px-6 py-3 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700"
                >
                    Create Your First Server
                </button>
            </div>
        {:else}
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {#each servers as _, i}
                    <ServerCard bind:server={servers[i]} />
                {/each}
            </div>
        {/if}
    </main>
</div>

<CreateServerModal bind:show={showCreateModal} onServerCreated={loadServers} />