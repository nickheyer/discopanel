<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { create } from '@bufbuild/protobuf';
	import { toast } from 'svelte-sonner';
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Separator } from '$lib/components/ui/separator';
	import * as Select from '$lib/components/ui/select';
	import {
		MousePointer2,
		Paintbrush,
		Replace,
		Copy,
		ClipboardPaste,
		Undo2,
		Loader2,
		RefreshCw,
		ZoomIn,
		ZoomOut,
		Map,
		Layers,
		Trash2,
		Square,
		ChevronUp,
		ChevronDown,
		Plus,
		Box,
		Grid2x2
	} from '@lucide/svelte';

	import { WorldViewer, type EditorTool } from '$lib/world-editor/world-viewer';
	import { getBlockColor, BLOCK_COLORS } from '$lib/world-editor/blocks';
	import type { WorldInfo, ClipboardData } from '$lib/proto/discopanel/v1/world_pb';
	import {
		ListWorldsRequestSchema,
		GetWorldInfoRequestSchema,
		GetChunksRequestSchema,
		SetBlocksRequestSchema,
		FillRegionRequestSchema,
		ReplaceBlocksRequestSchema,
		CopyRegionRequestSchema,
		PasteRegionRequestSchema,
		UndoRequestSchema,
		BlockPosSchema,
		BlockStateSchema,
		BlockChangeSchema
	} from '$lib/proto/discopanel/v1/world_pb';

	interface Props {
		serverId: string;
		active?: boolean;
	}

	let { serverId, active = false }: Props = $props();

	// State
	let viewerContainer: HTMLDivElement | null = $state(null);
	let viewer: WorldViewer | null = $state(null);
	let loading = $state(true);
	let loadingChunks = $state(false);

	// World data
	let worlds = $state<WorldInfo[]>([]);
	let selectedWorld = $state<string>('world');
	let selectedDimension = $state<string>('overworld');
	let worldInfo = $state<WorldInfo | null>(null);
	let dimensions = $state<string[]>(['overworld']);
	let minY = $state(-64);
	let maxY = $state(320);

	// Editor state
	let currentTool = $state<EditorTool>('select');
	let selectedBlock = $state<string>('minecraft:stone');
	let blockInput = $state<string>('');
	let yLevel = $state(64);
	let is3DMode = $state(false);

	// Recent blocks - dynamically populated as user enters block IDs
	let recentBlocks = $state<string[]>([]);

	// Selection
	let selectionFrom = $state<{ x: number; y: number; z: number } | null>(null);
	let selectionTo = $state<{ x: number; y: number; z: number } | null>(null);
	let hoveredBlock = $state<{ x: number; y: number; z: number; blockName: string } | null>(null);

	// Clipboard & history
	let clipboard = $state<ClipboardData | null>(null);
	let operations = $state<string[]>([]);

	// View settings
	let centerX = $state(0);
	let centerZ = $state(0);
	let viewRadius = $state(3);

	// Stats
	let chunkCount = $state(0);

	// Known block IDs for autocomplete
	const knownBlockIds = Object.keys(BLOCK_COLORS);

	onMount(async () => {
		if (active) {
			await initialize();
		}
	});

	$effect(() => {
		if (active && !viewer && viewerContainer) {
			initialize();
		}
	});

	onDestroy(() => {
		if (viewer) {
			viewer.dispose();
			viewer = null;
		}
	});

	async function initialize() {
		if (!viewerContainer) return;

		loading = true;

		try {
			viewer = new WorldViewer({
				container: viewerContainer,
				onBlockSelect: handleBlockSelect,
				onBlockHover: handleBlockHover,
				onSelectionChange: handleSelectionChange
			});

			await loadWorlds();
		} catch (error) {
			console.error('Failed to initialize world editor:', error);
			toast.error('Failed to initialize world editor');
		} finally {
			loading = false;
		}
	}

	async function loadWorlds() {
		try {
			const request = create(ListWorldsRequestSchema, { serverId });
			const response = await rpcClient.world.listWorlds(request);
			worlds = response.worlds;

			if (worlds.length > 0) {
				selectedWorld = worlds[0].name;
				await loadWorldInfo();
			}
		} catch (error) {
			console.error('Failed to load worlds:', error);
		}
	}

	async function loadWorldInfo() {
		try {
			const request = create(GetWorldInfoRequestSchema, {
				serverId,
				worldName: selectedWorld
			});
			const response = await rpcClient.world.getWorldInfo(request);
			worldInfo = response.world ?? null;
			dimensions = response.dimensions;
			minY = response.minY;
			maxY = response.maxY;

			if (viewer) {
				viewer.setWorldYBounds(minY, maxY);
			}

			// Focus on spawn
			if (worldInfo && viewer) {
				centerX = Math.floor(worldInfo.spawnX / 16);
				centerZ = Math.floor(worldInfo.spawnZ / 16);
				yLevel = worldInfo.spawnY;
				viewer.setYLevel(yLevel);
				viewer.focusOnPosition(worldInfo.spawnX, worldInfo.spawnY, worldInfo.spawnZ);
			}

			await loadChunks();
		} catch (error) {
			console.error('Failed to load world info:', error);
		}
	}

	async function loadChunks() {
		if (!viewer || loadingChunks) return;

		loadingChunks = true;

		try {
			const request = create(GetChunksRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				centerX,
				centerZ,
				radius: viewRadius,
				compact: true
			});

			const response = await rpcClient.world.getChunks(request);

			if (response.compactChunks.length > 0) {
				viewer.loadChunks(response.compactChunks);
			} else if (response.chunks.length > 0) {
				viewer.loadChunks(response.chunks);
			}

			chunkCount = viewer.getChunkCount();
		} catch (error) {
			console.error('Failed to load chunks:', error);
			toast.error('Failed to load chunks');
		} finally {
			loadingChunks = false;
		}
	}

	function handleBlockSelect(position: { x: number; y: number; z: number }, button: number) {
		if (button !== 0) return;

		if (currentTool === 'place') {
			placeBlock(position);
		}
	}

	function handleBlockHover(pos: { x: number; y: number; z: number; blockName: string } | null) {
		hoveredBlock = pos;
	}

	function handleSelectionChange(
		from: { x: number; y: number; z: number } | null,
		to: { x: number; y: number; z: number } | null
	) {
		selectionFrom = from;
		selectionTo = to;
	}

	async function placeBlock(position: { x: number; y: number; z: number }) {
		try {
			const request = create(SetBlocksRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				changes: [
					create(BlockChangeSchema, {
						position: create(BlockPosSchema, position),
						block: create(BlockStateSchema, { name: selectedBlock })
					})
				]
			});

			const response = await rpcClient.world.setBlocks(request);
			operations = [...operations, response.operationId];

			toast.success(`Placed block at ${position.x}, ${position.y}, ${position.z}`);
			await loadChunks();
		} catch (error) {
			console.error('Failed to place block:', error);
			toast.error('Failed to place block');
		}
	}

	async function fillSelection() {
		if (!selectionFrom || !selectionTo) {
			toast.error('No selection - right-drag to select a region');
			return;
		}

		try {
			const request = create(FillRegionRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				from: create(BlockPosSchema, selectionFrom),
				to: create(BlockPosSchema, selectionTo),
				block: create(BlockStateSchema, { name: selectedBlock })
			});

			const response = await rpcClient.world.fillRegion(request);
			operations = [...operations, response.operationId];

			toast.success(`Filled ${response.blocksChanged} blocks with ${formatBlockName(selectedBlock)}`);
			await loadChunks();
		} catch (error) {
			console.error('Failed to fill region:', error);
			toast.error('Failed to fill region');
		}
	}

	async function replaceInSelection() {
		if (!selectionFrom || !selectionTo) {
			toast.error('No selection - right-drag to select a region');
			return;
		}
		if (!hoveredBlock) {
			toast.error('Hover over a block to use as the replace target');
			return;
		}

		try {
			const request = create(ReplaceBlocksRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				from: create(BlockPosSchema, selectionFrom),
				to: create(BlockPosSchema, selectionTo),
				find: create(BlockStateSchema, { name: hoveredBlock.blockName }),
				replace: create(BlockStateSchema, { name: selectedBlock })
			});

			const response = await rpcClient.world.replaceBlocks(request);
			operations = [...operations, response.operationId];

			toast.success(`Replaced ${response.blocksReplaced} ${formatBlockName(hoveredBlock.blockName)} with ${formatBlockName(selectedBlock)}`);
			await loadChunks();
		} catch (error) {
			console.error('Failed to replace blocks:', error);
			toast.error('Failed to replace blocks');
		}
	}

	function toggle3DMode() {
		is3DMode = !is3DMode;
		if (viewer) {
			viewer.set3DMode(is3DMode);
		}
	}

	async function copySelection() {
		if (!selectionFrom || !selectionTo) {
			toast.error('No selection - right-drag to select a region');
			return;
		}

		try {
			const request = create(CopyRegionRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				from: create(BlockPosSchema, selectionFrom),
				to: create(BlockPosSchema, selectionTo)
			});

			const response = await rpcClient.world.copyRegion(request);
			clipboard = response.clipboard ?? null;

			toast.success(`Copied ${clipboard?.width}x${clipboard?.height}x${clipboard?.depth} region`);
		} catch (error) {
			console.error('Failed to copy region:', error);
			toast.error('Failed to copy region');
		}
	}

	async function pasteClipboard() {
		if (!clipboard || !hoveredBlock) {
			toast.error('No clipboard or target position');
			return;
		}

		try {
			const request = create(PasteRegionRequestSchema, {
				serverId,
				worldName: selectedWorld,
				dimension: selectedDimension,
				clipboard,
				position: create(BlockPosSchema, {
					x: hoveredBlock.x,
					y: hoveredBlock.y,
					z: hoveredBlock.z
				}),
				ignoreAir: true
			});

			const response = await rpcClient.world.pasteRegion(request);
			operations = [...operations, response.operationId];

			toast.success(`Pasted ${response.blocksPasted} blocks`);
			await loadChunks();
		} catch (error) {
			console.error('Failed to paste:', error);
			toast.error('Failed to paste');
		}
	}

	async function undoLast() {
		if (operations.length === 0) {
			toast.error('Nothing to undo');
			return;
		}

		const opId = operations[operations.length - 1];

		try {
			const request = create(UndoRequestSchema, {
				serverId,
				worldName: selectedWorld,
				operationId: opId
			});

			const response = await rpcClient.world.undo(request);

			if (response.success) {
				operations = operations.slice(0, -1);
				toast.success(`Reverted ${response.blocksReverted} blocks`);
				await loadChunks();
			}
		} catch (error) {
			console.error('Failed to undo:', error);
			toast.error('Failed to undo');
		}
	}

	function clearSelection() {
		if (viewer) {
			viewer.clearSelection();
		}
		selectionFrom = null;
		selectionTo = null;
	}

	function setTool(tool: EditorTool) {
		currentTool = tool;
		if (viewer) {
			viewer.setTool(tool);
		}
	}

	function setBlockId(blockId: string) {
		const normalized = blockId.includes(':') ? blockId : `minecraft:${blockId}`;
		selectedBlock = normalized;
		addToRecent(normalized);
	}

	function addToRecent(blockId: string) {
		if (!recentBlocks.includes(blockId)) {
			recentBlocks = [blockId, ...recentBlocks.slice(0, 11)];
		}
	}

	function addBlockFromInput() {
		if (blockInput.trim()) {
			setBlockId(blockInput.trim());
			blockInput = '';
		}
	}

	function handleYLevelChange(newY: number) {
		yLevel = Math.max(minY, Math.min(maxY, newY));
		if (viewer) {
			viewer.setYLevel(yLevel);
		}
	}

	function zoomIn() {
		if (viewer) {
			viewer.setZoom(viewer.getZoom() * 1.5);
		}
	}

	function zoomOut() {
		if (viewer) {
			viewer.setZoom(viewer.getZoom() / 1.5);
		}
	}

	function formatBlockName(name: string): string {
		return name.replace('minecraft:', '').replace(/_/g, ' ');
	}

	function getFilteredBlockIds(query: string): string[] {
		if (!query) return knownBlockIds.slice(0, 20);
		const q = query.toLowerCase().replace('minecraft:', '');
		return knownBlockIds.filter((id) => id.toLowerCase().includes(q)).slice(0, 20);
	}
</script>

<div class="h-full flex flex-col gap-2">
	<!-- Top toolbar -->
	<div class="flex flex-wrap items-center gap-2 p-2 bg-muted/50 rounded-lg">
		<!-- World/Dimension selectors -->
		<div class="flex items-center gap-1">
			<Map class="h-4 w-4 text-muted-foreground" />
			<Select.Root
				type="single"
				onValueChange={(v) => {
					selectedWorld = v;
					loadWorldInfo();
				}}
			>
				<Select.Trigger class="w-28 h-7 text-xs">{selectedWorld}</Select.Trigger>
				<Select.Content>
					{#each worlds as world}
						<Select.Item value={world.name}>{world.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>

		<div class="flex items-center gap-1">
			<Layers class="h-4 w-4 text-muted-foreground" />
			<Select.Root
				type="single"
				onValueChange={(v) => {
					selectedDimension = v;
					loadChunks();
				}}
			>
				<Select.Trigger class="w-24 h-7 text-xs">{selectedDimension}</Select.Trigger>
				<Select.Content>
					{#each dimensions as dim}
						<Select.Item value={dim}>{dim}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>

		<Separator orientation="vertical" class="h-5" />

		<!-- Y Level control -->
		<div class="flex items-center gap-1">
			<Label class="text-xs text-muted-foreground">Y:</Label>
			<Button variant="ghost" size="icon" class="h-6 w-6" onclick={() => handleYLevelChange(yLevel - 1)}>
				<ChevronDown class="h-3 w-3" />
			</Button>
			<Input
				type="number"
				bind:value={yLevel}
				onchange={(e) => handleYLevelChange(parseInt(e.currentTarget.value) || 64)}
				class="h-7 w-14 text-xs text-center"
				min={minY}
				max={maxY}
			/>
			<Button variant="ghost" size="icon" class="h-6 w-6" onclick={() => handleYLevelChange(yLevel + 1)}>
				<ChevronUp class="h-3 w-3" />
			</Button>
		</div>

		<Separator orientation="vertical" class="h-5" />

		<!-- Tools -->
		<div class="flex items-center gap-1">
			<Button
				variant={currentTool === 'select' ? 'default' : 'ghost'}
				size="icon"
				class="h-7 w-7"
				onclick={() => setTool('select')}
				title="Select (right-drag to select)"
			>
				<MousePointer2 class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant={currentTool === 'place' ? 'default' : 'ghost'}
				size="icon"
				class="h-7 w-7"
				onclick={() => setTool('place')}
				title="Place block (left-click)"
			>
				<Paintbrush class="h-3.5 w-3.5" />
			</Button>
		</div>

		<Separator orientation="vertical" class="h-5" />

		<!-- Actions -->
		<div class="flex items-center gap-1">
			<Button
				variant="ghost"
				size="icon"
				class="h-7 w-7"
				onclick={fillSelection}
				disabled={!selectionFrom || !selectionTo}
				title="Fill selection"
			>
				<Square class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant="ghost"
				size="icon"
				class="h-7 w-7"
				onclick={replaceInSelection}
				disabled={!selectionFrom || !selectionTo}
				title="Replace in selection"
			>
				<Replace class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant="ghost"
				size="icon"
				class="h-7 w-7"
				onclick={copySelection}
				disabled={!selectionFrom || !selectionTo}
				title="Copy selection"
			>
				<Copy class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant="ghost"
				size="icon"
				class="h-7 w-7"
				onclick={pasteClipboard}
				disabled={!clipboard}
				title="Paste"
			>
				<ClipboardPaste class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant="ghost"
				size="icon"
				class="h-7 w-7"
				onclick={undoLast}
				disabled={operations.length === 0}
				title="Undo"
			>
				<Undo2 class="h-3.5 w-3.5" />
			</Button>
		</div>

		<div class="flex-1"></div>

		<!-- View mode toggle -->
		<div class="flex items-center gap-1">
			<Button
				variant={!is3DMode ? 'default' : 'ghost'}
				size="icon"
				class="h-7 w-7"
				onclick={toggle3DMode}
				title="2D top-down view"
			>
				<Grid2x2 class="h-3.5 w-3.5" />
			</Button>
			<Button
				variant={is3DMode ? 'default' : 'ghost'}
				size="icon"
				class="h-7 w-7"
				onclick={toggle3DMode}
				title="3D inspection view"
			>
				<Box class="h-3.5 w-3.5" />
			</Button>
		</div>

		<Separator orientation="vertical" class="h-5" />

		<!-- Zoom -->
		<div class="flex items-center gap-1">
			<Button variant="ghost" size="icon" class="h-7 w-7" onclick={zoomOut} title="Zoom out">
				<ZoomOut class="h-3.5 w-3.5" />
			</Button>
			<Button variant="ghost" size="icon" class="h-7 w-7" onclick={zoomIn} title="Zoom in">
				<ZoomIn class="h-3.5 w-3.5" />
			</Button>
		</div>

		<Button
			variant="ghost"
			size="icon"
			class="h-7 w-7"
			onclick={loadChunks}
			disabled={loadingChunks}
			title="Refresh"
		>
			{#if loadingChunks}
				<Loader2 class="h-3.5 w-3.5 animate-spin" />
			{:else}
				<RefreshCw class="h-3.5 w-3.5" />
			{/if}
		</Button>

		<Badge variant="secondary" class="text-xs">{chunkCount} chunks</Badge>
	</div>

	<div class="flex-1 flex gap-2 min-h-0">
		<!-- Main viewer -->
		<div class="flex-1 relative rounded-lg overflow-hidden border bg-background">
			{#if loading}
				<div class="absolute inset-0 flex items-center justify-center bg-background/80 z-10">
					<Loader2 class="h-8 w-8 animate-spin text-primary" />
				</div>
			{/if}
			<div bind:this={viewerContainer} class="w-full h-full"></div>

			<!-- Status bar -->
			<div class="absolute bottom-2 left-2 right-2 flex items-center justify-between text-xs">
				{#if hoveredBlock}
					<div class="px-2 py-1 bg-background/90 rounded border">
						<span class="font-mono">{hoveredBlock.x}, {hoveredBlock.y}, {hoveredBlock.z}</span>
						<span class="text-muted-foreground ml-2">{formatBlockName(hoveredBlock.blockName)}</span>
					</div>
				{:else}
					<div></div>
				{/if}

				<div class="px-2 py-1 bg-background/90 rounded border text-muted-foreground">
					Scroll: zoom | Shift+drag or middle-click: pan | Right-drag: select
				</div>
			</div>
		</div>

		<!-- Side panel -->
		<div class="w-56 flex flex-col gap-2 overflow-y-auto">
			<!-- Block selector -->
			<Card>
				<CardHeader class="py-2 px-3">
					<CardTitle class="text-xs">Block ID</CardTitle>
				</CardHeader>
				<CardContent class="px-3 pb-3 space-y-2">
					<div class="flex gap-1">
						<Input
							type="text"
							bind:value={blockInput}
							placeholder="minecraft:stone"
							class="h-7 text-xs flex-1"
							onkeydown={(e) => e.key === 'Enter' && addBlockFromInput()}
							list="block-suggestions"
						/>
						<Button variant="ghost" size="icon" class="h-7 w-7" onclick={addBlockFromInput}>
							<Plus class="h-3 w-3" />
						</Button>
					</div>
					<datalist id="block-suggestions">
						{#each getFilteredBlockIds(blockInput) as id}
							<option value={id}></option>
						{/each}
					</datalist>

					<div class="flex items-center gap-2 p-1.5 rounded bg-muted/50">
						<div
							class="w-6 h-6 rounded border flex-shrink-0"
							style="background-color: #{getBlockColor(selectedBlock).toString(16).padStart(6, '0')}"
						></div>
						<span class="text-xs truncate flex-1">{formatBlockName(selectedBlock)}</span>
					</div>

					{#if recentBlocks.length > 0}
						<div class="flex flex-wrap gap-1">
							{#each recentBlocks as block}
								<button
									class="w-5 h-5 rounded border hover:ring-1 ring-primary transition-all"
									class:ring-1={selectedBlock === block}
									class:ring-primary={selectedBlock === block}
									style="background-color: #{getBlockColor(block).toString(16).padStart(6, '0')}"
									onclick={() => setBlockId(block)}
									title={block}
								></button>
							{/each}
						</div>
					{/if}
				</CardContent>
			</Card>

			<!-- View settings -->
			<Card>
				<CardHeader class="py-2 px-3">
					<CardTitle class="text-xs">Load Area</CardTitle>
				</CardHeader>
				<CardContent class="px-3 pb-3 space-y-2">
					<div class="grid grid-cols-2 gap-2">
						<div>
							<Label class="text-xs text-muted-foreground">Center X</Label>
							<Input type="number" bind:value={centerX} class="h-7 text-xs" />
						</div>
						<div>
							<Label class="text-xs text-muted-foreground">Center Z</Label>
							<Input type="number" bind:value={centerZ} class="h-7 text-xs" />
						</div>
					</div>
					<div>
						<Label class="text-xs text-muted-foreground">Radius: {viewRadius} chunks</Label>
						<input
							type="range"
							bind:value={viewRadius}
							min={1}
							max={8}
							step={1}
							class="mt-1 w-full"
						/>
					</div>
					<Button size="sm" class="w-full h-7 text-xs" onclick={loadChunks} disabled={loadingChunks}>
						{#if loadingChunks}
							<Loader2 class="h-3 w-3 mr-1 animate-spin" />
						{/if}
						Load Chunks
					</Button>
				</CardContent>
			</Card>

			<!-- Selection info -->
			{#if selectionFrom && selectionTo}
				<Card>
					<CardHeader class="py-2 px-3">
						<CardTitle class="text-xs">Selection</CardTitle>
					</CardHeader>
					<CardContent class="px-3 pb-3 space-y-1 text-xs">
						<div class="flex justify-between">
							<span class="text-muted-foreground">From</span>
							<span class="font-mono"
								>{selectionFrom.x}, {selectionFrom.y}, {selectionFrom.z}</span
							>
						</div>
						<div class="flex justify-between">
							<span class="text-muted-foreground">To</span>
							<span class="font-mono">{selectionTo.x}, {selectionTo.y}, {selectionTo.z}</span>
						</div>
						<div class="flex justify-between">
							<span class="text-muted-foreground">Size</span>
							<span class="font-mono">
								{Math.abs(selectionTo.x - selectionFrom.x) + 1}x{Math.abs(
									selectionTo.z - selectionFrom.z
								) + 1}
							</span>
						</div>
						<Button
							size="sm"
							variant="outline"
							class="w-full h-6 text-xs mt-1"
							onclick={clearSelection}
						>
							<Trash2 class="h-3 w-3 mr-1" />
							Clear
						</Button>
					</CardContent>
				</Card>
			{/if}

			<!-- Clipboard info -->
			{#if clipboard}
				<Card>
					<CardHeader class="py-2 px-3">
						<CardTitle class="text-xs">Clipboard</CardTitle>
					</CardHeader>
					<CardContent class="px-3 pb-3 text-xs">
						<span class="font-mono">{clipboard.width}x{clipboard.height}x{clipboard.depth}</span>
						<span class="text-muted-foreground ml-1">({clipboard.palette.length} types)</span>
					</CardContent>
				</Card>
			{/if}
		</div>
	</div>
</div>
