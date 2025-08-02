<script lang="ts">
    import { api } from '$lib/api';
    
    let { 
        show = $bindable(false),
        onServerCreated
    }: { 
        show: boolean;
        onServerCreated?: () => void;
    } = $props();
    
    let creating = $state(false);
    let formData = $state({
        name: '',
        description: '',
        mod_loader: 'vanilla',
        mc_version: '1.20.1',
        port: 25565,
        max_players: 20,
        memory: 2048
    });
    
    const modLoaders = [
        { value: 'vanilla', label: 'Vanilla' },
        { value: 'forge', label: 'Forge' },
        { value: 'fabric', label: 'Fabric' },
        { value: 'neoforge', label: 'NeoForge' },
        { value: 'paper', label: 'Paper' },
        { value: 'spigot', label: 'Spigot' }
    ];
    
    const versions = [
        '1.20.1',
        '1.20',
        '1.19.4',
        '1.19.2',
        '1.18.2',
        '1.17.1',
        '1.16.5'
    ];
    
    async function handleCreate() {
        if (!formData.name) return;
        
        creating = true;
        try {
            await api.createServer(formData);
            show = false;
            // Reset form
            formData = {
                name: '',
                description: '',
                mod_loader: 'vanilla',
                mc_version: '1.20.1',
                port: 25565,
                max_players: 20,
                memory: 2048
            };
            onServerCreated?.();
        } catch (err) {
            console.error('Failed to create server:', err);
            alert('Failed to create server: ' + (err instanceof Error ? err.message : 'Unknown error'));
        } finally {
            creating = false;
        }
    }
    
    function handleClose() {
        if (!creating) {
            show = false;
        }
    }
</script>

{#if show}
    <div class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center p-4 z-50">
        <div class="bg-white rounded-lg max-w-2xl w-full p-6 max-h-[90vh] overflow-y-auto">
            <h2 class="text-xl font-semibold mb-4">Create New Server</h2>
            
            <form onsubmit={e => { e.preventDefault(); handleCreate(); }}>
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div class="md:col-span-2">
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Server Name <span class="text-red-500">*</span>
                        </label>
                        <input
                            type="text"
                            bind:value={formData.name}
                            required
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            placeholder="My Minecraft Server"
                        />
                    </div>
                    
                    <div class="md:col-span-2">
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Description
                        </label>
                        <textarea
                            bind:value={formData.description}
                            rows="2"
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            placeholder="A fun Minecraft server for friends"
                        />
                    </div>
                    
                    <div>
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Mod Loader
                        </label>
                        <select
                            bind:value={formData.mod_loader}
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                        >
                            {#each modLoaders as loader}
                                <option value={loader.value}>{loader.label}</option>
                            {/each}
                        </select>
                    </div>
                    
                    <div>
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Minecraft Version
                        </label>
                        <select
                            bind:value={formData.mc_version}
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                        >
                            {#each versions as version}
                                <option value={version}>{version}</option>
                            {/each}
                        </select>
                    </div>
                    
                    <div>
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Port
                        </label>
                        <input
                            type="number"
                            bind:value={formData.port}
                            min="1"
                            max="65535"
                            required
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                        />
                    </div>
                    
                    <div>
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Max Players
                        </label>
                        <input
                            type="number"
                            bind:value={formData.max_players}
                            min="1"
                            max="1000"
                            required
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                        />
                    </div>
                    
                    <div class="md:col-span-2">
                        <label class="block text-sm font-medium text-gray-700 mb-1">
                            Memory (MB)
                        </label>
                        <input
                            type="number"
                            bind:value={formData.memory}
                            min="512"
                            max="32768"
                            step="512"
                            required
                            class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                        />
                        <p class="text-sm text-gray-500 mt-1">
                            Recommended: 2048 MB for vanilla, 4096 MB for modded
                        </p>
                    </div>
                </div>
                
                <div class="flex gap-3 mt-6">
                    <button
                        type="submit"
                        disabled={creating || !formData.name}
                        class="px-4 py-2 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {creating ? 'Creating...' : 'Create Server'}
                    </button>
                    <button
                        type="button"
                        onclick={handleClose}
                        disabled={creating}
                        class="px-4 py-2 bg-gray-200 text-gray-800 font-medium rounded-md hover:bg-gray-300 disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        Cancel
                    </button>
                </div>
            </form>
        </div>
    </div>
{/if}