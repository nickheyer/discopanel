<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { api } from '$lib/api/client';
	import { toast } from 'svelte-sonner';
	import { Loader2, Save, AlertCircle } from '@lucide/svelte';
	import type { Server, UpdateServerRequest, MinecraftVersion, ModLoaderInfo, DockerImageInfo } from '$lib/api/types';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';

	interface Props {
		server: Server;
		onUpdate?: () => void;
	}

	let { server, onUpdate }: Props = $props();

	let loading = $state(false);
	let saving = $state(false);
	let isDirty = $state(false);
	
	// Form state using UpdateServerRequest type
	let formData = $state<UpdateServerRequest>({
		name: server.name,
		description: server.description || '',
		max_players: server.max_players,
		memory: server.memory,
		mod_loader: server.mod_loader,
		mc_version: server.mc_version,
		java_version: server.java_version,
		docker_image: server.docker_image
	});

	// Available options
	let minecraftVersions = $state<MinecraftVersion | null>(null);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImageInfo[]>([]);
	let loadingOptions = $state(true);

	// Load available options
	$effect(() => {
		loadOptions();
	});

	// Watch for changes
	$effect(() => {
		isDirty = 
			formData.name !== server.name ||
			formData.description !== (server.description || '') ||
			formData.max_players !== server.max_players ||
			formData.memory !== server.memory ||
			formData.mod_loader !== server.mod_loader ||
			formData.mc_version !== server.mc_version ||
			formData.java_version !== server.java_version ||
			formData.docker_image !== server.docker_image;
	});

	async function loadOptions() {
		try {
			loadingOptions = true;
			const [versions, loaders, images] = await Promise.all([
				api.getMinecraftVersions(),
				api.getModLoaders(),
				api.getDockerImages()
			]);
			
			minecraftVersions = versions;
			modLoaders = loaders.modloaders;
			dockerImages = images.images;
		} catch (error) {
			toast.error('Failed to load options');
		} finally {
			loadingOptions = false;
		}
	}

	async function handleSave() {
		if (!isDirty) return;

		saving = true;
		try {
			await api.updateServer(server.id, formData);
			toast.success('Server settings updated. Restart the server to apply changes.');
			onUpdate?.();
			isDirty = false;
		} catch (error) {
			toast.error('Failed to update server settings');
		} finally {
			saving = false;
		}
	}

	function getCompatibleModLoaders(mcVersion: string): ModLoaderInfo[] {
		return modLoaders.filter(loader => 
			!loader.SupportedVersions || 
			loader.SupportedVersions.length === 0 ||
			loader.SupportedVersions.includes(mcVersion)
		);
	}

	// Update java version when MC version changes
	// $effect(() => {
	// 	if (formData.mc_version) {
	// 		const majorMinor = formData.mc_version.split('.').slice(0, 2).join('.');
	// 		const majorVersion = parseFloat(majorMinor);
			
	// 		if (majorVersion >= 1.20) {
	// 			formData.java_version = '21';
	// 		} else if (majorVersion >= 1.17) {
	// 			formData.java_version = '17';
	// 		} else if (majorVersion >= 1.12) {
	// 			formData.java_version = '8';
	// 		} else {
	// 			formData.java_version = '8';
	// 		}
	// 	}
	// });
</script>

<div class="space-y-6">
	{#if server.status !== 'stopped'}
		<Alert>
			<AlertCircle class="h-4 w-4" />
			<AlertDescription>
				Server must be stopped to modify these settings. Changes will take effect after restart.
			</AlertDescription>
		</Alert>
	{/if}

	<div class="grid gap-4 md:grid-cols-2">
		<div class="space-y-2">
			<Label for="name">Server Name</Label>
			<Input
				id="name"
				bind:value={formData.name}
				placeholder="My Server"
			/>
		</div>

		<div class="space-y-2">
			<Label for="description">Description</Label>
			<Input
				id="description"
				bind:value={formData.description}
				placeholder="A Minecraft server"
			/>
		</div>

		<div class="space-y-2">
			<Label for="memory">Memory (MB)</Label>
			<Input
				id="memory"
				type="number"
				bind:value={formData.memory}
				min="512"
				step="512"
			/>
		</div>

		<div class="space-y-2">
			<Label for="max_players">Max Players</Label>
			<Input
				id="max_players"
				type="number"
				bind:value={formData.max_players}
				min="1"
				max="100"
			/>
		</div>

		<div class="space-y-2">
			<Label for="mc_version">Minecraft Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.mc_version}
				onValueChange={(value: string | undefined) => formData.mc_version = value || ''}
			>
				<SelectTrigger id="mc_version">
					<span>{formData.mc_version || 'Select a version'}</span>
				</SelectTrigger>
				<SelectContent>
					{#if minecraftVersions}
						{#each minecraftVersions.versions as version}
							<SelectItem value={version}>{version}</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="mod_loader">Mod Loader</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.mod_loader}
				onValueChange={(value: string | undefined) => formData.mod_loader = value || ''}
			>
				<SelectTrigger id="mod_loader">
					<span>{formData.mod_loader || 'Select a mod loader'}</span>
				</SelectTrigger>
				<SelectContent>
					{#if formData.mc_version}
						{#each getCompatibleModLoaders(formData.mc_version || '') as loader}
							<SelectItem value={loader.Name}>
								{loader.DisplayName}
							</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="java_version">Java Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.java_version}
				onValueChange={(value: string | undefined) => formData.java_version = value || ''}
			>
				<SelectTrigger id="java_version">
					<span>{formData.java_version || 'Select Java version'}</span>
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="8">Java 8</SelectItem>
					<SelectItem value="11">Java 11</SelectItem>
					<SelectItem value="17">Java 17</SelectItem>
					<SelectItem value="21">Java 21</SelectItem>
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="docker_image">Docker Image</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.docker_image}
				onValueChange={(value: string | undefined) => formData.docker_image = value || ''}
			>
				<SelectTrigger id="docker_image">
					<span>{formData.docker_image || 'Select Docker image'}</span>
				</SelectTrigger>
				<SelectContent>
					{#each dockerImages as image}
						<SelectItem value={image.tag}>
							{image.tag} - Java {image.javaVersion} ({image.linux})
						</SelectItem>
					{/each}
				</SelectContent>
			</Select>
		</div>
	</div>

	<div class="flex justify-end">
		<Button 
			onclick={handleSave} 
			disabled={!isDirty || saving}
		>
			{#if saving}
				<Loader2 class="h-4 w-4 mr-2 animate-spin" />
			{:else}
				<Save class="h-4 w-4 mr-2" />
			{/if}
			Save Changes
		</Button>
	</div>
</div>