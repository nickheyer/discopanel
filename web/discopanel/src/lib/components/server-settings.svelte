<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
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
		docker_image: server.docker_image,
		detached: !!(server.detached),
		auto_start: !!(server.auto_start)
	});

	// Available options
	let minecraftVersions = $state<MinecraftVersion | null>(null);
	let modLoaders = $state<ModLoaderInfo[]>([]);
	let dockerImages = $state<DockerImageInfo[]>([]);
	let loadingOptions = $state(true);

	// Reset state when server changes
	let previousServerId = $state(server.id);
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;
			// Reset form data to match new server
			formData = {
				name: server.name,
				description: server.description || '',
				max_players: server.max_players,
				memory: server.memory,
				mod_loader: server.mod_loader,
				mc_version: server.mc_version,
				java_version: server.java_version,
				docker_image: server.docker_image,
				detached: !!(server.detached),
				auto_start: !!(server.auto_start)
			};
			saving = false;
			isDirty = false;
			// Reload options for new server
			loadOptions();
		}
	});

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
			formData.docker_image !== server.docker_image ||
			formData.detached !== !!(server.detached) ||
			formData.auto_start !== !!(server.auto_start);
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
		<Alert class="border-warning/50 bg-warning/10">
			<AlertCircle class="h-4 w-4 text-warning" />
			<AlertDescription class="text-sm">
				Server must be stopped to modify these settings. Changes will take effect after restart.
			</AlertDescription>
		</Alert>
	{/if}

	<div class="grid gap-6 md:grid-cols-2">
		<div class="space-y-2">
			<Label for="name" class="text-sm font-medium">Server Name</Label>
			<Input
				id="name"
				bind:value={formData.name}
				placeholder="My Server"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="description" class="text-sm font-medium">Description</Label>
			<Input
				id="description"
				bind:value={formData.description}
				placeholder="A Minecraft server"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="memory" class="text-sm font-medium">Memory (MB)</Label>
			<Input
				id="memory"
				type="number"
				bind:value={formData.memory}
				min="512"
				step="512"
				class="h-10"
			/>
			<p class="text-xs text-muted-foreground">
				Recommended: {formData.mod_loader === 'forge' || formData.mod_loader === 'neoforge' ? '4096' : formData.mod_loader === 'fabric' ? '3072' : '2048'} MB
			</p>
		</div>

		<div class="space-y-2">
			<Label for="max_players" class="text-sm font-medium">Max Players</Label>
			<Input
				id="max_players"
				type="number"
				bind:value={formData.max_players}
				min="1"
				max="100"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="mc_version" class="text-sm font-medium">Minecraft Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.mc_version}
				onValueChange={(value: string | undefined) => formData.mc_version = value || ''}
			>
				<SelectTrigger id="mc_version" class="h-10">
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
			<Label for="mod_loader" class="text-sm font-medium">Mod Loader</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.mod_loader}
				onValueChange={(value: string | undefined) => formData.mod_loader = value || ''}
			>
				<SelectTrigger id="mod_loader" class="h-10">
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
			<Label for="java_version" class="text-sm font-medium">Java Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.java_version}
				onValueChange={(value: string | undefined) => formData.java_version = value || ''}
			>
				<SelectTrigger id="java_version" class="h-10">
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
			<Label for="docker_image" class="text-sm font-medium">Docker Image <span class="text-muted-foreground text-xs">(Advanced)</span></Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== 'stopped'}
				value={formData.docker_image}
				onValueChange={(value: string | undefined) => formData.docker_image = value || ''}
			>
				<SelectTrigger id="docker_image" class="h-10">
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

		<div class="space-y-4">
			<h4 class="text-sm font-semibold">Lifecycle Management</h4>
			
			<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
				<div class="space-y-0.5">
					<Label for="detached" class="text-sm font-medium cursor-pointer">Detached Mode</Label>
					<p class="text-xs text-muted-foreground">
						Server continues running when DiscoPanel stops (not available for proxied servers)
					</p>
				</div>
				<Switch
					id="detached"
					checked={formData.detached}
					disabled={server.proxy_hostname !== ''}
					onCheckedChange={(checked) => {
						if (checked && server.proxy_hostname !== '') {
							toast.error("Cannot detach proxied servers");
							formData.detached = false;
							return;
						}
						formData.detached = checked;
						// If detaching, disable auto-start
						if (checked) {
							formData.auto_start = false;
						}
					}}
				/>
			</div>

			<div class="flex items-center justify-between p-4 rounded-lg bg-muted/50">
				<div class="space-y-0.5">
					<Label for="auto_start" class="text-sm font-medium cursor-pointer">Auto Start</Label>
					<p class="text-xs text-muted-foreground">
						Automatically start when DiscoPanel starts{formData.detached ? ' (disabled for detached servers)' : ''}
					</p>
				</div>
				<Switch
					id="auto_start"
					checked={formData.auto_start}
					disabled={formData.detached}
					onCheckedChange={(checked) => {
						if (formData.detached) {
							toast.error("Cannot enable auto-start for detached servers");
							formData.auto_start = false;
							return;
						}
						formData.auto_start = checked;
					}}
				/>
			</div>
		</div>
	</div>

	<Separator class="my-4" />



	<div class="flex justify-end pt-2">
		<Button 
			onclick={handleSave} 
			disabled={!isDirty || saving}
			size="sm"
			class="min-w-[120px]"
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