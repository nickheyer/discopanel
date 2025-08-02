<script lang="ts">
    import { onMount } from 'svelte';
    import { page } from '$app/state';
    import { api, type Server, type ServerConfig, type Mod } from '$lib/api';
    
    let server = $state<Server | null>(null);
    let config = $state<ServerConfig | null>(null);
    let mods = $state<Mod[]>([]);
    let logs = $state('');
    let activeTab = $state<'overview' | 'config' | 'mods' | 'logs'>('overview');
    let loading = $state(true);
    let error = $state<string | null>(null);
    
    let serverId = $derived(page?.params?.id || '');
    
    async function loadServerData() {
        try {
            loading = true;
            error = null;
            
            const [serverData, configData, modsData] = await Promise.all([
                api.getServer(serverId),
                api.getServerConfig(serverId),
                api.listMods(serverId)
            ]);
            
            server = serverData;
            config = configData;
            mods = modsData;
        } catch (err) {
            error = err instanceof Error ? err.message : 'Failed to load server data';
        } finally {
            loading = false;
        }
    }
    
    async function loadLogs() {
        try {
            const data = await api.getServerLogs(serverId);
            logs = data.logs;
        } catch (err) {
            console.error('Failed to load logs:', err);
            logs = 'Failed to load logs';
        }
    }
    
    async function handleConfigUpdate(field: string, value: any) {
        if (!config) return;
        
        try {
            const updates = { [field]: value };
            config = await api.updateServerConfig(serverId, updates);
        } catch (err) {
            error = 'Failed to update configuration';
        }
    }
    
    async function toggleMod(mod: Mod) {
        try {
            await api.updateMod(serverId, mod.id, { enabled: !mod.enabled });
            mod.enabled = !mod.enabled;
        } catch (err) {
            console.error('Failed to toggle mod:', err);
        }
    }
    
    async function deleteMod(modId: string) {
        try {
            await api.deleteMod(serverId, modId);
            mods = mods.filter(m => m.id !== modId);
        } catch (err) {
            console.error('Failed to delete mod:', err);
        }
    }
    
    onMount(() => {
        loadServerData();
        const interval = setInterval(() => {
            loadServerData();
            if (activeTab === 'logs') {
                loadLogs();
            }
        }, 5000);
        return () => clearInterval(interval);
    });
    
    $effect(() => {
        if (activeTab === 'logs' && !logs) {
            loadLogs();
        }
    });
</script>

<div class="min-h-screen bg-gray-50">
    <header class="bg-white shadow-sm border-b border-gray-200">
        <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
            <div class="flex justify-between items-center">
                <div class="flex items-center gap-4">
                    <a href="/" class="text-gray-500 hover:text-gray-700">
                        ← Back
                    </a>
                    <h1 class="text-2xl font-bold text-gray-900">
                        {server?.name || 'Loading...'}
                    </h1>
                    {#if server}
                        <span class="px-3 py-1 text-xs font-medium rounded-full border
                            {server.status === 'running' ? 'text-green-600 bg-green-50 border-green-200' :
                             server.status === 'stopped' ? 'text-gray-600 bg-gray-50 border-gray-200' :
                             server.status === 'starting' ? 'text-blue-600 bg-blue-50 border-blue-200' :
                             server.status === 'stopping' ? 'text-yellow-600 bg-yellow-50 border-yellow-200' :
                             'text-red-600 bg-red-50 border-red-200'}">
                            {server.status}
                        </span>
                    {/if}
                </div>
            </div>
        </div>
    </header>
    
    {#if error}
        <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 mt-4">
            <div class="p-4 bg-red-50 border border-red-200 rounded-md">
                <p class="text-red-700">{error}</p>
            </div>
        </div>
    {/if}
    
    {#if loading}
        <div class="flex justify-center items-center h-64">
            <div class="text-gray-500">Loading server details...</div>
        </div>
    {:else if server}
        <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
            <!-- Tabs -->
            <div class="border-b border-gray-200 mb-6">
                <nav class="-mb-px flex space-x-8">
                    <button
                        onclick={() => activeTab = 'overview'}
                        class="py-2 px-1 border-b-2 font-medium text-sm
                            {activeTab === 'overview' 
                                ? 'border-blue-500 text-blue-600' 
                                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'}"
                    >
                        Overview
                    </button>
                    <button
                        onclick={() => activeTab = 'config'}
                        class="py-2 px-1 border-b-2 font-medium text-sm
                            {activeTab === 'config' 
                                ? 'border-blue-500 text-blue-600' 
                                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'}"
                    >
                        Configuration
                    </button>
                    <button
                        onclick={() => activeTab = 'mods'}
                        class="py-2 px-1 border-b-2 font-medium text-sm
                            {activeTab === 'mods' 
                                ? 'border-blue-500 text-blue-600' 
                                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'}"
                    >
                        Mods ({mods.length})
                    </button>
                    <button
                        onclick={() => activeTab = 'logs'}
                        class="py-2 px-1 border-b-2 font-medium text-sm
                            {activeTab === 'logs' 
                                ? 'border-blue-500 text-blue-600' 
                                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'}"
                    >
                        Logs
                    </button>
                </nav>
            </div>
            
            <!-- Tab Content -->
            {#if activeTab === 'overview'}
                <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div class="bg-white p-6 rounded-lg shadow-sm border border-gray-200">
                        <h3 class="text-lg font-medium mb-4">Server Information</h3>
                        <dl class="space-y-3">
                            <div>
                                <dt class="text-sm text-gray-500">Minecraft Version</dt>
                                <dd class="text-sm font-medium">{server.mc_version}</dd>
                            </div>
                            <div>
                                <dt class="text-sm text-gray-500">Mod Loader</dt>
                                <dd class="text-sm font-medium">{server.mod_loader}</dd>
                            </div>
                            <div>
                                <dt class="text-sm text-gray-500">Port</dt>
                                <dd class="text-sm font-medium">{server.port}</dd>
                            </div>
                            <div>
                                <dt class="text-sm text-gray-500">Memory Allocation</dt>
                                <dd class="text-sm font-medium">{server.memory} MB</dd>
                            </div>
                            <div>
                                <dt class="text-sm text-gray-500">Max Players</dt>
                                <dd class="text-sm font-medium">{server.max_players}</dd>
                            </div>
                        </dl>
                    </div>
                    
                    <div class="bg-white p-6 rounded-lg shadow-sm border border-gray-200">
                        <h3 class="text-lg font-medium mb-4">Quick Actions</h3>
                        <div class="space-y-3">
                            {#if server.status === 'stopped'}
                                <button
                                    onclick={() => api.startServer(serverId).then(loadServerData)}
                                    class="w-full px-4 py-2 bg-green-600 text-white font-medium rounded-md hover:bg-green-700"
                                >
                                    Start Server
                                </button>
                            {:else if server.status === 'running'}
                                <button
                                    onclick={() => api.stopServer(serverId).then(loadServerData)}
                                    class="w-full px-4 py-2 bg-red-600 text-white font-medium rounded-md hover:bg-red-700"
                                >
                                    Stop Server
                                </button>
                                <button
                                    onclick={() => api.restartServer(serverId).then(loadServerData)}
                                    class="w-full px-4 py-2 bg-yellow-600 text-white font-medium rounded-md hover:bg-yellow-700"
                                >
                                    Restart Server
                                </button>
                            {/if}
                        </div>
                    </div>
                </div>
            {:else if activeTab === 'config' && config}
                <div class="bg-white p-6 rounded-lg shadow-sm border border-gray-200">
                    <h3 class="text-lg font-medium mb-4">Server Configuration</h3>
                    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div>
                            <label class="block text-sm font-medium text-gray-700 mb-1">MOTD</label>
                            <input
                                type="text"
                                value={config.motd}
                                onchange={(e) => handleConfigUpdate('motd', e.currentTarget.value)}
                                class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            />
                        </div>
                        
                        <div>
                            <label class="block text-sm font-medium text-gray-700 mb-1">Difficulty</label>
                            <select
                                value={config.difficulty}
                                onchange={(e) => handleConfigUpdate('difficulty', e.currentTarget.value)}
                                class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            >
                                <option value="peaceful">Peaceful</option>
                                <option value="easy">Easy</option>
                                <option value="normal">Normal</option>
                                <option value="hard">Hard</option>
                            </select>
                        </div>
                        
                        <div>
                            <label class="block text-sm font-medium text-gray-700 mb-1">Game Mode</label>
                            <select
                                value={config.gamemode}
                                onchange={(e) => handleConfigUpdate('gamemode', e.currentTarget.value)}
                                class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            >
                                <option value="survival">Survival</option>
                                <option value="creative">Creative</option>
                                <option value="adventure">Adventure</option>
                                <option value="spectator">Spectator</option>
                            </select>
                        </div>
                        
                        <div>
                            <label class="block text-sm font-medium text-gray-700 mb-1">View Distance</label>
                            <input
                                type="number"
                                value={config.view_distance}
                                min="2"
                                max="32"
                                onchange={(e) => handleConfigUpdate('view_distance', parseInt(e.currentTarget.value))}
                                class="w-full px-3 py-2 border border-gray-300 rounded-md focus:ring-blue-500 focus:border-blue-500"
                            />
                        </div>
                    </div>
                    
                    <div class="mt-6 space-y-3">
                        <label class="flex items-center">
                            <input
                                type="checkbox"
                                checked={config.pvp}
                                onchange={(e) => handleConfigUpdate('pvp', e.currentTarget.checked)}
                                class="mr-2"
                            />
                            <span class="text-sm">Enable PvP</span>
                        </label>
                        
                        <label class="flex items-center">
                            <input
                                type="checkbox"
                                checked={config.online_mode}
                                onchange={(e) => handleConfigUpdate('online_mode', e.currentTarget.checked)}
                                class="mr-2"
                            />
                            <span class="text-sm">Online Mode (Require valid Minecraft accounts)</span>
                        </label>
                        
                        <label class="flex items-center">
                            <input
                                type="checkbox"
                                checked={config.allow_nether}
                                onchange={(e) => handleConfigUpdate('allow_nether', e.currentTarget.checked)}
                                class="mr-2"
                            />
                            <span class="text-sm">Allow Nether</span>
                        </label>
                    </div>
                </div>
            {:else if activeTab === 'mods'}
                <div class="bg-white p-6 rounded-lg shadow-sm border border-gray-200">
                    <div class="flex justify-between items-center mb-4">
                        <h3 class="text-lg font-medium">Installed Mods</h3>
                        <button class="px-4 py-2 bg-blue-600 text-white font-medium rounded-md hover:bg-blue-700">
                            Upload Mod
                        </button>
                    </div>
                    
                    {#if mods.length === 0}
                        <p class="text-gray-500 text-center py-8">No mods installed yet</p>
                    {:else}
                        <div class="space-y-3">
                            {#each mods as mod}
                                <div class="flex items-center justify-between p-3 border border-gray-200 rounded-md">
                                    <div>
                                        <h4 class="font-medium">{mod.name}</h4>
                                        <p class="text-sm text-gray-500">
                                            {mod.file_name} • {mod.version || 'Unknown version'}
                                        </p>
                                    </div>
                                    <div class="flex items-center gap-2">
                                        <label class="flex items-center">
                                            <input
                                                type="checkbox"
                                                checked={mod.enabled}
                                                onchange={() => toggleMod(mod)}
                                                class="mr-2"
                                            />
                                            <span class="text-sm">Enabled</span>
                                        </label>
                                        <button
                                            onclick={() => deleteMod(mod.id)}
                                            class="text-red-600 hover:text-red-800 text-sm"
                                        >
                                            Delete
                                        </button>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {/if}
                </div>
            {:else if activeTab === 'logs'}
                <div class="bg-white p-6 rounded-lg shadow-sm border border-gray-200">
                    <div class="flex justify-between items-center mb-4">
                        <h3 class="text-lg font-medium">Server Logs</h3>
                        <button
                            onclick={loadLogs}
                            class="px-3 py-1 bg-gray-100 text-gray-700 text-sm font-medium rounded-md hover:bg-gray-200"
                        >
                            Refresh
                        </button>
                    </div>
                    <pre class="bg-gray-900 text-gray-100 p-4 rounded-md overflow-x-auto text-sm font-mono">{logs || 'Loading logs...'}</pre>
                </div>
            {/if}
        </div>
    {/if}
</div>