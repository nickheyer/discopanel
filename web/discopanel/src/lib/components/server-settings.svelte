<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import { Separator } from '$lib/components/ui/separator';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import { toast } from 'svelte-sonner';
	import { Loader2, Save, AlertCircle } from '@lucide/svelte';
	import type { Server, AdditionalPort, DockerOverrides } from '$lib/proto/discopanel/v1/common_pb';
	import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';
	import type { UpdateServerRequest } from '$lib/proto/discopanel/v1/server_pb';
	import { UpdateServerRequestSchema } from '$lib/proto/discopanel/v1/server_pb';
	import type { GetMinecraftVersionsResponse, GetModLoadersResponse, GetDockerImagesResponse } from '$lib/proto/discopanel/v1/minecraft_pb';
	import { GetMinecraftVersionsRequestSchema, GetModLoadersRequestSchema, GetDockerImagesRequestSchema } from '$lib/proto/discopanel/v1/minecraft_pb';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';

	interface Props {
		server: Server;
		onUpdate?: () => void;
	}

	let { server, onUpdate }: Props = $props();

	let saving = $state(false);
	let isDirty = $state(false);
	
	// Parse additional ports and docker overrides from JSON strings
	function parseAdditionalPorts(jsonStr?: string): AdditionalPort[] {
		if (!jsonStr) return [];
		try {
			return JSON.parse(jsonStr);
		} catch (e) {
			console.error('Failed to parse additionalPorts:', e);
			return [];
		}
	}

	function parseDockerOverrides(jsonStr?: string): DockerOverrides | undefined {
		if (!jsonStr) return undefined;
		try {
			return JSON.parse(jsonStr);
		} catch (e) {
			console.error('Failed to parse dockerOverrides:', e);
			return undefined;
		}
	}

	let formData = $state<UpdateServerRequest>(
		create(UpdateServerRequestSchema, {
			id: server.id,
			name: server.name,
			description: server.description || '',
			maxPlayers: server.maxPlayers,
			memory: server.memory,
			modLoader: String(server.modLoader),
			mcVersion: server.mcVersion,
			dockerImage: server.dockerImage,
			detached: server.detached,
			autoStart: server.autoStart,
			tpsCommand: server.tpsCommand || '',
			modpackId: '', // Not used in this context
			modpackVersionId: '', // Not used in this context
			additionalPorts: parseAdditionalPorts(server.additionalPorts),
			dockerOverrides: parseDockerOverrides(server.dockerOverrides)
		})
	);


	// Available options
	let minecraftVersions = $state<GetMinecraftVersionsResponse | null>(null);
	let modLoaders = $state<GetModLoadersResponse | null>(null);
	let dockerImages = $state<GetDockerImagesResponse | null>(null);
	let loadingOptions = $state(true);

	// Reset state when server changes
	let previousServerId = $state(server.id);
	$effect(() => {
		if (server.id !== previousServerId) {
			previousServerId = server.id;


			// Reset form data to match new server
			formData = create(UpdateServerRequestSchema, {
				id: server.id,
				name: server.name,
				description: server.description || '',
				maxPlayers: server.maxPlayers,
				memory: server.memory,
				modLoader: String(server.modLoader),
				mcVersion: server.mcVersion,
				dockerImage: server.dockerImage,
				detached: server.detached,
				autoStart: server.autoStart,
				tpsCommand: server.tpsCommand || '',
				modpackId: '', // Not used in this context
				modpackVersionId: '', // Not used in this context
				additionalPorts: parseAdditionalPorts(server.additionalPorts),
				dockerOverrides: parseDockerOverrides(server.dockerOverrides)
			});
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
			formData.maxPlayers !== server.maxPlayers ||
			formData.memory !== server.memory ||
			formData.modLoader !== String(server.modLoader) ||
			formData.mcVersion !== server.mcVersion ||
			formData.dockerImage !== server.dockerImage ||
			formData.detached !== server.detached ||
			formData.autoStart !== server.autoStart ||
			formData.tpsCommand !== (server.tpsCommand || '') ||
			JSON.stringify(formData.additionalPorts) !== JSON.stringify(parseAdditionalPorts(server.additionalPorts)) ||
			JSON.stringify(formData.dockerOverrides) !== JSON.stringify(parseDockerOverrides(server.dockerOverrides));
	});

	async function loadOptions() {
		try {
			loadingOptions = true;
			const [versions, loaders, images] = await Promise.all([
				rpcClient.minecraft.getMinecraftVersions(create(GetMinecraftVersionsRequestSchema, {})),
				rpcClient.minecraft.getModLoaders(create(GetModLoadersRequestSchema, {})),
				rpcClient.minecraft.getDockerImages(create(GetDockerImagesRequestSchema, {}))
			]);

			minecraftVersions = versions;
			modLoaders = loaders;
			dockerImages = images;
		} catch (error) {
			toast.error('Failed to load options');
		} finally {
			loadingOptions = false;
		}
	}

	function handleMemoryInput(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const value = Number(input.value);
		
		// Prevent negative values
		if (value < 0) {
			input.value = '512';
			formData.memory = 512;
		}
	}

	function handleMaxPlayersInput(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const value = Number(input.value);
		
		// Prevent negative values and zero
		if (value <= 0) {
			input.value = '1';
			formData.maxPlayers = 1;
		}
	}

	async function handleSave() {
		if (!isDirty) return;

		saving = true;
		try {
			const request = create(UpdateServerRequestSchema, formData);
			await rpcClient.server.updateServer(request);
			toast.success('Server settings updated. Restart the server to apply changes.');
			onUpdate?.();
			isDirty = false;
		} catch (error) {
			toast.error('Failed to update server settings');
		} finally {
			saving = false;
		}
	}

	function getCompatibleModLoaders(mcVersion: string) {
		// The proto doesn't include version compatibility info, so all loaders are shown
		// Backend has SupportedVersions field but it's not populated or sent via proto
		return modLoaders?.modloaders || [];
	}

</script>

<div class="space-y-6 p-4 overflow-y-auto h-full">
	{#if server.status !== ServerStatus.STOPPED}
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
				oninput={handleMemoryInput}
				min="512"
				class="h-10"
			/>
			<p class="text-xs text-muted-foreground">
				Recommended: {formData.modLoader === 'vanilla' ? '2048' : '4096'} MB
			</p>
		</div>

		<div class="space-y-2">
			<Label for="max_players" class="text-sm font-medium">Max Players</Label>
			<Input
				id="max_players"
				type="number"
				bind:value={formData.maxPlayers}
				oninput={handleMaxPlayersInput}
				min="1"
				max="1000"
				class="h-10"
			/>
		</div>

		<div class="space-y-2">
			<Label for="mc_version" class="text-sm font-medium">Minecraft Version</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.mcVersion}
				onValueChange={(value: string | undefined) => formData.mcVersion = value || ''}
			>
				<SelectTrigger id="mc_version" class="h-10">
					<span>{formData.mcVersion || 'Select a version'}</span>
				</SelectTrigger>
				<SelectContent>
					{#if minecraftVersions}
						{#each minecraftVersions.versions as version (version.id)}
							<SelectItem value={version.id}>{version.id}</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="mod_loader" class="text-sm font-medium">Mod Loader</Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.modLoader}
				onValueChange={(value: string | undefined) => formData.modLoader = value || ''}
			>
				<SelectTrigger id="mod_loader" class="h-10">
					<span>{formData.modLoader || 'Select a mod loader'}</span>
				</SelectTrigger>
				<SelectContent>
					{#if formData.mcVersion}
						{#each getCompatibleModLoaders(formData.mcVersion || '') as loader (loader.name)}
							<SelectItem value={loader.name}>
								{loader.displayName}
							</SelectItem>
						{/each}
					{/if}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="docker_image" class="text-sm font-medium">Docker Image <span class="text-muted-foreground text-xs">(Advanced)</span></Label>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.dockerImage}
				onValueChange={(value: string | undefined) => formData.dockerImage = value || ''}
			>
				<SelectTrigger id="docker_image" class="h-10">
					<span>{formData.dockerImage || 'Select Docker image'}</span>
				</SelectTrigger>
				<SelectContent>
					{#each dockerImages?.images || [] as image (image.tag)}
						<SelectItem value={image.tag}>
							{image.displayName || image.tag}
						</SelectItem>
					{/each}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-2">
			<Label for="tps_command" class="text-sm font-medium">TPS Command <span class="text-muted-foreground text-xs">(Optional)</span></Label>
			<Input
				id="tps_command"
				placeholder="Polling TPS command"
				bind:value={formData.tpsCommand}
				class="h-10"
			/>
			<p class="text-xs text-muted-foreground">
				Override the TPS monitoring command (empty to disable). Use " ?? " to specify fallback commands (e.g., "forge tps ?? neoforge tps ?? tps")
			</p>
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
					disabled={server.proxyHostname !== ''}
					onCheckedChange={(checked) => {
						if (checked && server.proxyHostname !== '') {
							toast.error("Cannot detach proxied servers");
							formData.detached = false;
							return;
						}
						formData.detached = checked;
						// If detaching, disable auto-start
						if (checked) {
							formData.autoStart = false;
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
					checked={formData.autoStart}
					disabled={formData.detached}
					onCheckedChange={(checked) => {
						if (formData.detached) {
							toast.error("Cannot enable auto-start for detached servers");
							formData.autoStart = false;
							return;
						}
						formData.autoStart = checked;
					}}
				/>
			</div>
		</div>
	</div>

	<Separator class="my-4" />

	<div class="space-y-4">
		<AdditionalPortsEditor
			bind:ports={formData.additionalPorts}
			disabled={saving}
			onchange={(ports) => formData.additionalPorts = ports}
		/>

		<DockerOverridesEditor
			bind:overrides={formData.dockerOverrides}
			disabled={saving}
			onchange={(overrides) => formData.dockerOverrides = overrides}
		/>
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