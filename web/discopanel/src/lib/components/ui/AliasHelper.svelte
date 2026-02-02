<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Popover, PopoverContent, PopoverTrigger } from '$lib/components/ui/popover';
	import { rpcClient } from '$lib/api/rpc-client';
	import { AliasCategory, type AliasInfo } from '$lib/proto/discopanel/v1/module_pb';
	import { Braces, Server, Box, Sparkles, Loader2, Check, Copy } from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { copyToClipboard } from '$lib/utils/clipboard';

	interface Props {
		serverId?: string;
		moduleId?: string;
		showLabel?: boolean;
	}

	let { serverId, moduleId, showLabel = false }: Props = $props();

	let aliases = $state<AliasInfo[]>([]);
	let loading = $state(false);
	let open = $state(false);

	async function loadAliases() {
		if (aliases.length > 0) return; // Already loaded
		loading = true;
		try {
			const response = await rpcClient.module.getAvailableAliases({
				serverId,
				moduleId
			});
			aliases = response.aliases;
		} catch (error) {
			console.error('Failed to load aliases:', error);
		} finally {
			loading = false;
		}
	}

	function handleOpenChange(isOpen: boolean) {
		open = isOpen;
		if (isOpen) {
			loadAliases();
		}
	}

	let copiedAlias = $state<string | null>(null);

	async function handleCopy(alias: string) {
		const success = await copyToClipboard(alias);
		if (success) {
			copiedAlias = alias;
			toast.success('Copied to clipboard', { description: alias });
			setTimeout(() => {
				copiedAlias = null;
			}, 2000);
		} else {
			toast.error('Failed to copy to clipboard');
		}
	}

	function getCategoryIcon(category: AliasCategory) {
		switch (category) {
			case AliasCategory.SERVER:
				return Server;
			case AliasCategory.MODULE:
				return Box;
			case AliasCategory.SPECIAL:
				return Sparkles;
			default:
				return Braces;
		}
	}

	function getCategoryLabel(category: AliasCategory): string {
		switch (category) {
			case AliasCategory.SERVER:
				return 'Server';
			case AliasCategory.MODULE:
				return 'Module';
			case AliasCategory.SPECIAL:
				return 'Special';
			default:
				return 'Other';
		}
	}

	function getCategoryColor(category: AliasCategory): string {
		switch (category) {
			case AliasCategory.SERVER:
				return 'text-blue-500';
			case AliasCategory.MODULE:
				return 'text-purple-500';
			case AliasCategory.SPECIAL:
				return 'text-amber-500';
			default:
				return 'text-gray-500';
		}
	}

	// Group aliases by category
	let groupedAliases = $derived.by(() => {
		const groups = new Map<AliasCategory, AliasInfo[]>();
		for (const alias of aliases) {
			if (!groups.has(alias.category)) {
				groups.set(alias.category, []);
			}
			groups.get(alias.category)!.push(alias);
		}
		return groups;
	});
</script>

<Popover bind:open onOpenChange={handleOpenChange}>
	<PopoverTrigger>
		{#if showLabel}
			<Button variant="outline" size="sm" class="h-7 text-xs gap-1.5">
				<Braces class="h-3.5 w-3.5" />
				Aliases
			</Button>
		{:else}
			<Button variant="ghost" size="icon" class="h-7 w-7" title="Available Aliases">
				<Braces class="h-4 w-4" />
			</Button>
		{/if}
	</PopoverTrigger>
	<PopoverContent class="w-96 max-h-96 overflow-y-auto p-0" align="end">
		<div class="p-3 border-b">
			<h4 class="font-medium text-sm">Available Aliases</h4>
			<p class="text-xs text-muted-foreground mt-1">Click to copy an alias to clipboard</p>
		</div>

		{#if loading}
			<div class="flex items-center justify-center py-8">
				<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
			</div>
		{:else}
			<div class="divide-y">
				{#each [...groupedAliases.entries()] as [category, categoryAliases]}
					{@const CategoryIcon = getCategoryIcon(category)}
					<div class="p-2">
						<div class="flex items-center gap-2 px-2 py-1.5 text-xs font-medium text-muted-foreground">
							<CategoryIcon class="h-3.5 w-3.5 {getCategoryColor(category)}" />
							{getCategoryLabel(category)}
						</div>
						<div class="space-y-1">
							{#each categoryAliases as alias}
								<button
									type="button"
									class="w-full text-left p-2 rounded-md hover:bg-muted/50 transition-colors group"
									onclick={() => handleCopy(alias.alias)}
								>
									<div class="flex items-center justify-between">
										<code class="text-xs font-mono text-primary bg-primary/10 px-1.5 py-0.5 rounded group-hover:bg-primary/20">
											{alias.alias}
										</code>
										<div class="flex items-center gap-2">
											{#if alias.exampleValue}
												<span class="text-xs text-muted-foreground font-mono truncate max-w-[100px]" title={alias.exampleValue}>
													= {alias.exampleValue}
												</span>
											{/if}
											{#if copiedAlias === alias.alias}
												<Check class="h-3.5 w-3.5 text-green-500" />
											{:else}
												<Copy class="h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
											{/if}
										</div>
									</div>
									<p class="text-xs text-muted-foreground mt-1 truncate">
										{alias.description}
									</p>
								</button>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</PopoverContent>
</Popover>
