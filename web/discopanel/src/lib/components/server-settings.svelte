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
	import { Loader2, Save, AlertCircle, WandSparkles } from '@lucide/svelte';
	import type { Server } from '$lib/proto/discopanel/v1/common_pb';
	import * as _ from 'lodash-es';
	import { ServerStatus, ModLoader, TPSExtractionMode } from '$lib/proto/discopanel/v1/common_pb';
	import type { UpdateServerRequest } from '$lib/proto/discopanel/v1/server_pb';
	import { SendCommandRequestSchema, UpdateServerRequestSchema } from '$lib/proto/discopanel/v1/server_pb';
	import type {
		GetMinecraftVersionsResponse,
		GetModLoadersResponse,
		GetDockerImagesResponse
	} from '$lib/proto/discopanel/v1/minecraft_pb';
	import {
		GetMinecraftVersionsRequestSchema,
		GetModLoadersRequestSchema,
		GetDockerImagesRequestSchema,

		GetTestTPSRegexRequestSchema

	} from '$lib/proto/discopanel/v1/minecraft_pb';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import AdditionalPortsEditor from '$lib/components/additional-ports-editor.svelte';
	import { compareMinecraftVersion, getUniqueDockerImages } from '$lib/utils';
	import DockerOverridesEditor from '$lib/components/docker-overrides-editor.svelte';
	import { enumToString } from '$lib/utils';
	import { Textarea } from './ui/textarea';
	import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog';

	interface Props {
		server: Server;
		onUpdate?: () => void;
	}

	let { server, onUpdate }: Props = $props();

	let saving = $state(false);

	function safeToString(data?: unknown): string | undefined {
		if (!data) return undefined;
		try {
			return JSON.stringify(data, (_, value) =>
				typeof value === 'bigint' ? value.toString() : value
			);
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
			port: server.port,
			maxPlayers: server.maxPlayers,
			memory: server.memory,
			modLoader: enumToString(ModLoader, server.modLoader),
			mcVersion: server.mcVersion,
			dockerImage: server.dockerImage,
			detached: server.detached,
			autoStart: server.autoStart,
			tpsEnabled: server.tpsEnabled,
			tpsCommand: server.tpsCommand || '',
			tpsExtractionMode: server.tpsExtractionMode,
			tpsCustomRegex: server.tpsCustomRegex || '',
			modpackId: '', // Not used in this context
			modpackVersionId: '', // Not used in this context
			additionalPorts: server.additionalPorts || [],
			dockerOverrides: server.dockerOverrides
		})
	);

	let isDirty = $derived(
		formData.name !== server.name ||
			formData.description !== (server.description || '') ||
			formData.port !== server.port ||
			formData.maxPlayers !== server.maxPlayers ||
			formData.memory !== server.memory ||
			formData.modLoader !== enumToString(ModLoader, server.modLoader) ||
			formData.mcVersion !== server.mcVersion ||
			formData.dockerImage !== server.dockerImage ||
			formData.detached !== server.detached ||
			formData.autoStart !== server.autoStart ||

			formData.tpsEnabled !== server.tpsEnabled ||
        	(formData.tpsEnabled && (
			    formData.tpsCommand !== (server.tpsCommand || '') ||
			    formData.tpsExtractionMode !== server.tpsExtractionMode ||
			    formData.tpsCustomRegex !== (server.tpsCustomRegex || '')
			)) ||


			safeToString(formData.additionalPorts) !== safeToString(server.additionalPorts || []) ||
			safeToString($state.snapshot(formData.dockerOverrides)) !==
				safeToString(server.dockerOverrides)
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
				port: server.port,
				maxPlayers: server.maxPlayers,
				memory: server.memory,
				modLoader: enumToString(ModLoader, server.modLoader),
				mcVersion: server.mcVersion,
				dockerImage: server.dockerImage,
				detached: server.detached,
				autoStart: server.autoStart,
				tpsEnabled: server.tpsEnabled,
				tpsCommand: server.tpsCommand || '',
				tpsExtractionMode: server.tpsExtractionMode,
				tpsCustomRegex: server.tpsCustomRegex || '',
				modpackId: '', // Not used in this context
				modpackVersionId: '', // Not used in this context
				additionalPorts: server.additionalPorts || [],
				dockerOverrides: server.dockerOverrides
			});
			saving = false;
			// Reload options for new server
			loadOptions();
		}
	});

	// Load available options
	$effect(() => {
		loadOptions();
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
		} catch (_e) {
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
		} catch (_e) {
			toast.error('Failed to update server settings');
		} finally {
			saving = false;
		}
	}

	function getCompatibleModLoaders(_mcVersion: string) {
		// The proto doesn't include version compatibility info, so all loaders are shown
		// Backend has SupportedVersions field but it's not populated or sent via proto
		return modLoaders?.modloaders || [];
	}

	function getRecommendedTpsSettings(modLoader: string) {
        switch (modLoader?.toLowerCase()) {
			case 'fabric':
            case 'vanilla':
				if (compareMinecraftVersion("1.20.3", server.mcVersion) > 0) {console.log("return"); return;}
                return {
                    command: 'tick query',
                    mode: TPSExtractionMode.TPSExtractionModeVanilla
                };
            case 'paper':
            case 'spigot':
            case 'purpur':
                return {
                    command: 'tps',
                    mode: TPSExtractionMode.TPSExtractionModeSpigot
                };
            case 'forge':
				return {
                    command: 'forge tps',
                    mode: TPSExtractionMode.TPSExtractionModeForge
                };
            case 'neoforge':
                return {
                    command: 'neoforge tps',
                    mode: TPSExtractionMode.TPSExtractionModeForge
                };
            default:
                return {
                    command: 'tps',
                    mode: TPSExtractionMode.TPSExtractionModeLegacy
                };
        }
    }

    let recommended = $derived(getRecommendedTpsSettings(formData.modLoader));
    let isNotRecommended = $derived(
        formData.tpsEnabled && recommended != undefined && (
            formData.tpsCommand !== recommended.command || 
            formData.tpsExtractionMode !== recommended.mode
        )
    );

    function applyRecommendedSettings() {
		if (recommended == undefined) return;
        formData.tpsCommand = recommended.command;
        formData.tpsExtractionMode = recommended.mode;
    }

	let showRegexModal = $state(false);
	let testInputText = $state("")
	let testRegex = $state(formData.tpsCustomRegex || "")
	let regexResult = $state<number|undefined>(undefined);

	async function openRegexModal() {
		showRegexModal = true;
		testRegex = formData.tpsCustomRegex || "";
		regexResult = undefined;
		if (server.status === ServerStatus.RUNNING) {
			const currentTPSCommand = formData.tpsCommand;
			if(!currentTPSCommand || currentTPSCommand == "") return;
			const request = create(SendCommandRequestSchema, {
				id: server.id,
				command: currentTPSCommand
			});
			const response = await rpcClient.server.sendCommand(request)
			testInputText = response.output;
		}
	}

	async function testTPSRegex(){
		const request = create(GetTestTPSRegexRequestSchema, {
			input: testInputText,
			regex: testRegex
		});
		const response = await rpcClient.minecraft.getTestTPSRegex(request)
		regexResult = response.tps;
	}

	function applyRegex(){
		showRegexModal = false;
		formData.tpsCustomRegex = testRegex;
	}
</script>

<div class="h-full space-y-6 overflow-y-auto p-4">
	{#if server.status !== ServerStatus.STOPPED}
		<Alert class="border-warning/50 bg-warning/10">
			<AlertCircle class="text-warning h-4 w-4" />
			<AlertDescription class="text-sm">
				Server must be stopped to modify these settings. Changes will take effect after restart.
			</AlertDescription>
		</Alert>
	{/if}

	<div class="grid gap-6 md:grid-cols-2">
		<div class="space-y-2">
			<Label for="name" class="text-sm font-medium">Server Name</Label>
			<Input id="name" bind:value={formData.name} placeholder="My Server" class="h-10" />
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
			<Label for="port" class="text-sm font-medium">Port</Label>
			<Input
				id="port"
				type="number"
				bind:value={formData.port}
				min="1"
				max="65535"
				disabled={server.proxyHostname !== ''}
				class="h-10"
			/>
			{#if server.proxyHostname}
				<p class="text-xs text-muted-foreground">
					Port cannot be changed for proxy-enabled servers
				</p>
			{/if}
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
				onValueChange={(value: string | undefined) => (formData.mcVersion = value || '')}
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
				onValueChange={(value: string) => (formData.modLoader = value)}
			>
				<SelectTrigger id="mod_loader" class="h-10">
					<span
						>{modLoaders?.modloaders?.find((l) => l.name === formData.modLoader)?.displayName ||
							_.startCase(formData.modLoader) ||
							'Select a mod loader'}</span
					>
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
			<Label for="docker_image" class="text-sm font-medium"
				>Docker Image <span class="text-xs text-muted-foreground">(Advanced)</span></Label
			>
			<Select
				type="single"
				disabled={loadingOptions || server.status !== ServerStatus.STOPPED}
				value={formData.dockerImage}
				onValueChange={(value: string | undefined) => (formData.dockerImage = value || '')}
			>
				<SelectTrigger id="docker_image" class="h-10">
					<span>{formData.dockerImage || 'Select Docker image'}</span>
				</SelectTrigger>
				<SelectContent>
					{#each getUniqueDockerImages(dockerImages?.images || []) as image (image.tag)}
						<SelectItem value={image.tag}>
							{image.displayName || image.tag}
						</SelectItem>
					{/each}
				</SelectContent>
			</Select>
		</div>

		<div class="space-y-4 rounded-lg border bg-muted/30 p-4">
		    <div class="flex items-center justify-between">
		        <div class="space-y-0.5">
		            <Label for="tps_enabled" class="cursor-pointer text-sm font-medium">
		                TPS Monitoring <span class="text-xs text-muted-foreground">(Optional)</span>
		            </Label>
		            <p class="text-xs text-muted-foreground">
		                Automated polling and parsing of server TPS metrics.
		            </p>
		        </div>
		        <div class="flex items-center gap-3">
		            <!-- Apply Recommended Button -->
		            {#if isNotRecommended && recommended != undefined}
		                <Button 
		                    type="button" 
		                    variant="outline" 
		                    size="sm" 
		                    class="h-7 px-2 text-xs gap-1 border-primary/30 hover:border-primary text-primary transition-all"
		                    onclick={applyRecommendedSettings}
		                >
		                    <WandSparkles class="h-3 w-3" />
		                    Apply Recommended
		                </Button>
		            {/if}
					
		            <Switch
		                id="tps_enabled"
		                checked={formData.tpsEnabled}
		                onCheckedChange={(checked) => (formData.tpsEnabled = checked)}
		            />
		        </div>
		    </div>
		
		    {#if formData.tpsEnabled}
		        <Separator />
		
		        <div class="space-y-4 pt-1">
		            <!-- Command Input -->
		            <div class="space-y-1.5">
		                <Label for="tps_command" class="text-xs font-medium">Command</Label>
		                <Input
		                    id="tps_command"
		                    placeholder="e.g. tick query"
		                    bind:value={formData.tpsCommand}
		                    class="h-9 text-sm"
		                />
		                <p class="text-[11px] text-muted-foreground">
		                    Command sent to console. Use <code class="rounded bg-muted px-1">??</code> for fallbacks (e.g. <code class="rounded bg-muted px-1">forge tps ?? tps</code>).
		                </p>
		            </div>
				
		            <!-- Extraction Mode Select -->
		            <div class="space-y-1.5">
		                <Label for="tps_mode" class="text-xs font-medium">Extraction Strategy</Label>
		                <Select
		                    type="single"
		                    value={String(formData.tpsExtractionMode)}
		                    onValueChange={(value: string) => {
		                        formData.tpsExtractionMode = value ? (Number(value) as TPSExtractionMode) : TPSExtractionMode.TPSExtractionModeLegacy;
		                    }}
		                >
		                    <SelectTrigger id="tps_mode" class="h-9 text-sm">
		                        <span>{TPSExtractionMode[formData.tpsExtractionMode] || 'Select extraction strategy'}</span>
		                    </SelectTrigger>
		                    <SelectContent>
		                        {#each Object.keys(TPSExtractionMode).filter(key => isNaN(Number(key))) as mode (mode)}
		                            <SelectItem value={String(TPSExtractionMode[mode as keyof typeof TPSExtractionMode])}>
		                                {mode}
		                            </SelectItem>
		                        {/each}
		                    </SelectContent>
		                </Select>
					
		                <!-- Info Help Texts based on selected Mode -->
		                <p class="text-[11px] text-muted-foreground">
		                    {#if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeVanilla}
		                        Parses TPS from standard Minecraft Vanilla command <code class="rounded bg-muted px-1">/tick query</code> outputs (Version 1.20.3 and newer).
		                    {:else if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeSpigot}
		                        Parses TPS from Paper/Spigot command <code class="rounded bg-muted px-1">/tps</code> outputs.
		                    {:else if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeForge}
		                        Parses TPS from Forge/NeoForge commands <code class="rounded bg-muted px-1">/forge tps</code> or <code class="rounded bg-muted px-1">/neoforge tps</code> outputs.
		                    {:else if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeCustom}
		                        Use a custom Regex to extract the TPS value from your command output.
		                    {:else if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeLegacy}
		                        Extract TPS based on Discopanels old extraction logic.
		                    {/if}
		                </p>
		            </div>
				
		            <!-- Custom Regex Input (Conditionally Rendered) -->
		            {#if formData.tpsExtractionMode === TPSExtractionMode.TPSExtractionModeCustom}
		                <div class="space-y-1.5 rounded-md border border-dashed p-3 bg-background/50">
							<div class="flex items-center justify-between">
								<Label for="tps_custom_regex" class="text-xs font-medium">Custom Regex Pattern</Label>
								<!-- Test Button -->
								<button
									type="button"
									onclick={openRegexModal}
									class="text-[11px] font-medium text-primary hover:underline flex items-center gap-1"
								>
									Test Your Regex
								</button>
							</div>
		                    <Input
		                        id="tps_custom_regex"
		                        placeholder="e.g. TPS:\s*([0-9.]+)"
		                        bind:value={formData.tpsCustomRegex}
		                        class="h-9 font-mono text-xs"
		                    />
		                    <p class="text-[11px] text-muted-foreground">
		                        Must contain a capture group <code class="rounded bg-muted px-1">(...)</code> pointing to the numerical TPS value. If you don't know what this means, don't use it.
		                    </p>
		                </div>
		            {/if}
		        </div>
		    {/if}
		</div>

		<div class="space-y-4">
			<h4 class="text-sm font-semibold">Lifecycle Management</h4>

			<div class="flex items-center justify-between rounded-lg bg-muted/50 p-4">
				<div class="space-y-0.5">
					<Label for="detached" class="cursor-pointer text-sm font-medium">Detached Mode</Label>
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
							toast.error('Cannot detach proxied servers');
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

			<div class="flex items-center justify-between rounded-lg bg-muted/50 p-4">
				<div class="space-y-0.5">
					<Label for="auto_start" class="cursor-pointer text-sm font-medium">Auto Start</Label>
					<p class="text-xs text-muted-foreground">
						Automatically start when DiscoPanel starts{formData.detached
							? ' (disabled for detached servers)'
							: ''}
					</p>
				</div>
				<Switch
					id="auto_start"
					checked={formData.autoStart}
					disabled={formData.detached}
					onCheckedChange={(checked) => {
						if (formData.detached) {
							toast.error('Cannot enable auto-start for detached servers');
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
			onchange={(ports) => (formData.additionalPorts = ports)}
		/>

		<DockerOverridesEditor
			bind:overrides={formData.dockerOverrides}
			disabled={saving}
			onchange={(overrides) => (formData.dockerOverrides = overrides)}
		/>
	</div>

	<Separator class="my-4" />

	<div class="flex justify-end pt-2">
		<Button onclick={handleSave} disabled={!isDirty || saving} size="sm" class="min-w-[120px]">
			{#if saving}
				<Loader2 class="mr-2 h-4 w-4 animate-spin" />
			{:else}
				<Save class="mr-2 h-4 w-4" />
			{/if}
			Save Changes
		</Button>
	</div>
</div>


<Dialog bind:open={showRegexModal}>
	<DialogContent class="sm:max-w-2xl w-full p-6">
		<DialogHeader class="border-b pb-2">
			<DialogTitle class="text-sm font-semibold">Regex Tester</DialogTitle>
		</DialogHeader>

		<div class="space-y-4 py-2">
			<!-- Regex Input -->
			<div class="space-y-1">
				<Label class="text-xs">Regex Pattern</Label>
				<Input 
					bind:value={testRegex} 
					class="h-8 font-mono text-xs" 
					placeholder="z. B. TPS:\s*([0-9.]+)" 
				/>
			</div>

			<!-- Test Input Text -->
			<div class="space-y-1">
				<Label class="text-xs">Example Text (Start your server and run your TPS command, paste the output here)</Label>
				<Textarea
					bind:value={testInputText}
					rows={6}
					class="font-mono text-xs"
					placeholder="Insert TPS Command output here..."
				/>
			</div>

			<!-- Test Button -->
			<Button 
				type="button" 
				variant="outline" 
				size="sm" 
				onclick={testTPSRegex}
			>
				Test
			</Button>

			<!-- Result Box -->
			<div class="space-y-1">
				<Label class="text-xs">Output / Extraction</Label>
				<div class="rounded-md border bg-muted/30 p-3 font-mono text-xs">
					{#if regexResult == undefined}
						<span class="font-medium text-white">Run test to show extracted TPS.</span>
					{:else if regexResult == 0.0}
						<span class="font-medium text-destructive">Could not extract TPS</span>
					{:else}
						<div class="space-y-1.5 text-green-500">
								{regexResult}
						</div>
					{/if}
				</div>
			</div>
		</div>

		<!-- Actions -->
		<DialogFooter class="border-t pt-3 gap-2 sm:justify-end">
			<Button
				type="button"
				variant="outline"
				size="sm"
				onclick={() => (showRegexModal = false)}
			>
				Cancel
			</Button>
			<Button
				type="button"
				size="sm"
				onclick={applyRegex}
			>
				Apply pattern
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>