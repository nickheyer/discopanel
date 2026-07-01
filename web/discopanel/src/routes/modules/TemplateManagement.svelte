<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import DynamicIcon from '$lib/components/ui/DynamicIcon.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleTemplateType } from '$lib/proto/discopanel/v1/module_pb';
	import { Loader2, Plus, Trash2, Settings, Package, RefreshCw, Layers } from '@lucide/svelte';
	import ModuleTemplateCreateDialog from '$lib/components/server/ModuleTemplateCreateDialog.svelte';
	import { onMount } from 'svelte';

	let templates = $state<ModuleTemplate[]>([]);
	let loading = $state(true);

	// Dialog state
	let createDialogOpen = $state(false);
	let editDialogOpen = $state(false);
	let selectedTemplate = $state<ModuleTemplate | null>(null);

	onMount(() => {
		loadTemplates();
	});

	async function loadTemplates(silent = false) {
		try {
			if (!silent) loading = true;
			const response = await rpcClient.module.listModuleTemplates({});
			templates = response.templates;
		} catch {
			if (!silent) toast.error('Failed to load module templates');
		} finally {
			if (!silent) loading = false;
		}
	}

	async function handleDeleteTemplate(template: ModuleTemplate) {
		const confirmed = confirm(
			`Are you sure you want to delete template "${template.name}"?\n\nThis cannot be undone and will not affect existing instances.`
		);
		if (!confirmed) return;

		try {
			await rpcClient.module.deleteModuleTemplate({ id: template.id });
			toast.success(`Template "${template.name}" deleted`);
			await loadTemplates(true);
		} catch (error) {
			toast.error(
				`Failed to delete template: ${error instanceof Error ? error.message : 'Unknown error'}`
			);
		}
	}

	function openEditDialog(template: ModuleTemplate) {
		selectedTemplate = template;
		editDialogOpen = true;
	}

	let categories = $derived.by(() => {
		const cats = new Set<string>();
		templates.forEach((t) => {
			if (t.category) cats.add(t.category);
		});
		return Array.from(cats).sort();
	});

	let selectedCategory = $state<string | null>(null);

	let filteredTemplates = $derived.by(() => {
		if (!selectedCategory) return templates;
		return templates.filter((t) => t.category === selectedCategory);
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h3 class="text-lg font-medium">Module Templates</h3>
			<p class="text-sm text-muted-foreground">Manage blueprints for creating module instances.</p>
		</div>
		<div class="flex items-center gap-2">
			<Button variant="outline" size="sm" onclick={() => loadTemplates()} disabled={loading}>
				{#if loading}
					<Loader2 class="h-4 w-4 animate-spin" />
				{:else}
					<RefreshCw class="h-4 w-4" />
				{/if}
			</Button>
			<Button onclick={() => (createDialogOpen = true)}>
				<Plus class="mr-2 h-4 w-4" />
				Create Template
			</Button>
		</div>
	</div>

	{#if categories.length > 0}
		<div class="flex flex-wrap gap-2">
			<Button
				variant={selectedCategory === null ? 'default' : 'outline'}
				size="sm"
				onclick={() => (selectedCategory = null)}
				class="h-8 px-3"
			>
				All
			</Button>
			{#each categories as cat (cat)}
				<Button
					variant={selectedCategory === cat ? 'default' : 'outline'}
					size="sm"
					onclick={() => (selectedCategory = cat)}
					class="h-8 px-3"
				>
					{cat}
				</Button>
			{/each}
		</div>
	{/if}

	{#if loading && templates.length === 0}
		<div class="flex items-center justify-center py-12">
			<Loader2 class="h-8 w-8 animate-spin text-muted-foreground" />
		</div>
	{:else if templates.length === 0}
		<div
			class="flex flex-col items-center justify-center rounded-lg border bg-card py-12 text-center"
		>
			<Layers class="mb-4 h-12 w-12 text-muted-foreground/50" />
			<h3 class="mb-1 text-lg font-medium">No Templates Found</h3>
			<p class="mb-4 max-w-sm text-sm text-muted-foreground">
				You don't have any module templates configured yet.
			</p>
			<Button onclick={() => (createDialogOpen = true)}>
				<Plus class="mr-2 h-4 w-4" />
				Create Template
			</Button>
		</div>
	{:else}
		<div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredTemplates as template (template.name)}
				<Card
					class="group relative overflow-hidden border shadow-sm transition-all hover:shadow-md"
				>
					<CardContent class="flex h-full flex-col p-5">
						<div class="mb-4 flex items-start gap-4">
							<div
								class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-primary/10"
							>
								<DynamicIcon name={template.icon} class="h-6 w-6 text-primary" fallback="Package" />
							</div>
							<div class="min-w-0 flex-1">
								<h3 class="truncate text-lg font-semibold">{template.name}</h3>
								<div class="mt-1 flex flex-wrap items-center gap-2">
									{#if template.type === ModuleTemplateType.BUILTIN}
										<Badge variant="default" class="px-1.5 py-0 text-[10px]">Built-in</Badge>
									{:else}
										<Badge variant="outline" class="px-1.5 py-0 text-[10px]">Custom</Badge>
									{/if}
									{#if template.category}
										<Badge variant="secondary" class="px-1.5 py-0 text-[10px]"
											>{template.category}</Badge
										>
									{/if}
								</div>
							</div>
						</div>

						<p class="mb-4 line-clamp-2 flex-1 text-sm text-muted-foreground">
							{template.description || 'No description provided'}
						</p>

						<div class="mt-auto flex items-center justify-between border-t pt-4">
							<div class="max-w-[150px] truncate font-mono text-xs text-muted-foreground">
								{template.dockerImage}
							</div>

							{#if template.type === ModuleTemplateType.CUSTOM}
								<div class="flex items-center gap-1">
									<Button
										size="icon"
										variant="ghost"
										onclick={() => openEditDialog(template)}
										title="Edit template"
										class="h-8 w-8"
									>
										<Settings class="h-4 w-4" />
									</Button>
									<Button
										size="icon"
										variant="ghost"
										onclick={() => handleDeleteTemplate(template)}
										title="Delete template"
										class="h-8 w-8 text-destructive hover:text-destructive"
									>
										<Trash2 class="h-4 w-4" />
									</Button>
								</div>
							{:else}
								<div class="rounded bg-muted px-2 py-1 text-xs text-muted-foreground/50">
									Read-only
								</div>
							{/if}
						</div>
					</CardContent>
				</Card>
			{/each}
		</div>
	{/if}
</div>

<ModuleTemplateCreateDialog
	bind:open={createDialogOpen}
	mode="create"
	onSuccess={() => loadTemplates(true)}
/>

{#if selectedTemplate}
	<ModuleTemplateCreateDialog
		bind:open={editDialogOpen}
		mode="edit"
		template={selectedTemplate}
		onSuccess={() => loadTemplates(true)}
	/>
{/if}
