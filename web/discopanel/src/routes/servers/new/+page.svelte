<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { api } from '$lib/api/client';
	import { serversStore } from '$lib/stores/servers';
	import { toast } from 'svelte-sonner';
	import { ArrowLeft, Loader2 } from '@lucide/svelte';
	import type { CreateServerRequest, ModLoader, MinecraftVersion, ModLoaderInfo } from '$lib/api/types';

	let loading = $state(false);
	let loadingVersions = $state(true);
	let minecraftVersions = $state<string[]>([]);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let latestVersion = $state('');

	let formData = $state<CreateServerRequest>({
		name: '',
		description: '',
		mod_loader: 'vanilla',
		mc_version: '',
		port: 25565,
		max_players: 20,
		memory: 2048,
		auto_start: false
	});

	onMount(async () => {
		try {
			const [versionsData, loadersData] = await Promise.all([
				api.getMinecraftVersions(),
				api.getModLoaders()
			]);
			
			minecraftVersions = versionsData.versions;
			latestVersion = versionsData.latest;
			modLoaders = loadersData.modloaders;
			
			if (!formData.mc_version && latestVersion) {
				formData.mc_version = latestVersion;
			}
		} catch (error) {
			toast.error('Failed to load Minecraft versions');
			console.error(error);
		} finally {
			loadingVersions = false;
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		
		if (!formData.name.trim()) {
			toast.error('Server name is required');
			return;
		}

		loading = true;
		try {
			const server = await api.createServer(formData);
			serversStore.addServer(server);
			toast.success(`Server "${server.name}" created successfully!`);
			goto(`/servers/${server.id}`);
		} catch (error) {
			toast.error(`Failed to create server: ${error instanceof Error ? error.message : 'Unknown error'}`);
		} finally {
			loading = false;
		}
	}

	function getRecommendedMemory(): number {
		switch (formData.mod_loader) {
			case 'vanilla':
				return 1024;
			case 'paper':
			case 'spigot':
				return 2048;
			case 'fabric':
				return 3072;
			case 'forge':
			case 'neoforge':
				return 4096;
			default:
				return 2048;
		}
	}

	function setRecommendedMemory() {
		formData.memory = getRecommendedMemory();
	}

	$effect(() => {
		// Auto-adjust memory when mod loader changes
		if (formData.memory === 2048 || formData.memory === getRecommendedMemory()) {
			setRecommendedMemory();
		}
	});
</script>

<div class="flex-1 space-y-4 p-8 pt-6">
	<div class="flex items-center space-x-2">
		<Button variant="ghost" size="icon" href="/servers">
			<ArrowLeft class="h-4 w-4" />
		</Button>
		<div>
			<h2 class="text-3xl font-bold tracking-tight">Create Server</h2>
			<p class="text-muted-foreground">Set up a new Minecraft server instance</p>
		</div>
	</div>

	<form onsubmit={handleSubmit}>
		<div class="grid gap-4 md:grid-cols-2">
			<Card>
				<CardHeader>
					<CardTitle>Basic Information</CardTitle>
					<CardDescription>Configure your server's basic settings</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div class="space-y-2">
						<Label for="name">Server Name</Label>
						<Input
							id="name"
							placeholder="My Awesome Server"
							bind:value={formData.name}
							required
							disabled={loading}
						/>
					</div>

					<div class="space-y-2">
						<Label for="description">Description</Label>
						<Textarea
							id="description"
							placeholder="A fun server for friends..."
							bind:value={formData.description}
							disabled={loading}
						/>
					</div>

					<Separator />

					<div class="space-y-2">
						<Label for="mc_version">Minecraft Version</Label>
						{#if loadingVersions}
							<div class="flex items-center justify-center p-4">
								<Loader2 class="h-4 w-4 animate-spin" />
							</div>
						{:else}
							<Select type="single" value={formData.mc_version} onValueChange={(v: string | undefined) => formData.mc_version = v ?? ''} disabled={loading}>
								<SelectTrigger id="mc_version">
									<span>{formData.mc_version || 'Select a version'}</span>
								</SelectTrigger>
								<SelectContent>
									{#each minecraftVersions as version}
										<SelectItem value={version}>
											{version} {version === latestVersion ? '(Latest)' : ''}
										</SelectItem>
									{/each}
								</SelectContent>
							</Select>
						{/if}
					</div>

					<div class="space-y-2">
						<Label for="mod_loader">Mod Loader</Label>
						<Select type="single" value={formData.mod_loader} onValueChange={(v: string | undefined) => formData.mod_loader = (v as ModLoader) ?? 'vanilla'} disabled={loading}>
							<SelectTrigger id="mod_loader">
								<span>{modLoaders.find(l => l.Name === formData.mod_loader)?.DisplayName || 'Select a mod loader'}</span>
							</SelectTrigger>
							<SelectContent>
								{#each modLoaders as loader}
									<SelectItem value={loader.Name}>
										{loader.DisplayName}
									</SelectItem>
								{/each}
							</SelectContent>
						</Select>
						{#if formData.mod_loader === 'vanilla'}
							<p class="text-sm text-muted-foreground">
								No mod support - vanilla Minecraft server
							</p>
						{:else if modLoaders.find(l => l.Name === formData.mod_loader)?.ModsDirectory}
							<p class="text-sm text-muted-foreground">
								Mods will be stored in: {modLoaders.find(l => l.Name === formData.mod_loader)?.ModsDirectory}/
							</p>
						{/if}
					</div>
				</CardContent>
			</Card>

			<Card>
				<CardHeader>
					<CardTitle>Server Configuration</CardTitle>
					<CardDescription>Fine-tune your server's performance and network settings</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div class="space-y-2">
						<Label for="port">Server Port</Label>
						<Input
							id="port"
							type="number"
							min="1"
							max="65535"
							bind:value={formData.port}
							disabled={loading}
						/>
						<p class="text-sm text-muted-foreground">
							Default Minecraft port is 25565
						</p>
					</div>

					<div class="space-y-2">
						<Label for="max_players">Max Players</Label>
						<Input
							id="max_players"
							type="number"
							min="1"
							max="1000"
							bind:value={formData.max_players}
							disabled={loading}
						/>
					</div>

					<div class="space-y-2">
						<Label for="memory">Memory Allocation (MB)</Label>
						<div class="flex space-x-2">
							<Input
								id="memory"
								type="number"
								min="512"
								max="32768"
								step="512"
								bind:value={formData.memory}
								disabled={loading}
							/>
							<Button
								type="button"
								variant="outline"
								onclick={setRecommendedMemory}
								disabled={loading}
							>
								Recommended
							</Button>
						</div>
						<p class="text-sm text-muted-foreground">
							Recommended: {getRecommendedMemory()} MB for {formData.mod_loader}
						</p>
					</div>

					<Separator />

					<div class="flex items-center justify-between">
						<div class="space-y-0.5">
							<Label for="auto_start">Auto Start</Label>
							<p class="text-sm text-muted-foreground">
								Automatically start the server when DiscoPanel starts
							</p>
						</div>
						<Switch
							id="auto_start"
							bind:checked={formData.auto_start}
							disabled={loading}
						/>
					</div>
				</CardContent>
			</Card>
		</div>

		<div class="flex justify-end space-x-2 mt-6">
			<Button variant="outline" href="/servers" disabled={loading}>
				Cancel
			</Button>
			<Button type="submit" disabled={loading || loadingVersions}>
				{#if loading}
					<Loader2 class="h-4 w-4 mr-2 animate-spin" />
					Creating...
				{:else}
					Create Server
				{/if}
			</Button>
		</div>
	</form>
</div>