<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { EmptyState, ConfirmDialog } from '$lib/components/app';
	import DynamicIcon from '$lib/components/ui/DynamicIcon.svelte';
	import { rpcClient } from '$lib/api/rpc-client';
	import { toast } from 'svelte-sonner';
	import type { ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';
	import { ModuleTemplateType } from '$lib/proto/discopanel/v1/module_pb';
	import { Plus, Trash2, Settings, RefreshCw, Layers, Search } from '@lucide/svelte';
	import ModuleTemplateCreateDialog from '$lib/components/server/ModuleTemplateCreateDialog.svelte';
	import { onMount } from 'svelte';

	let templates = $state<ModuleTemplate[]>([]);
	let loading = $state(true);

	let createDialogOpen = $state(false);
	let editDialogOpen = $state(false);
	let selectedTemplate = $state<ModuleTemplate | null>(null);
	let deleteTarget = $state<ModuleTemplate | null>(null);
	let deleteOpen = $state(false);

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

	function requestDelete(template: ModuleTemplate) {
		deleteTarget = template;
		deleteOpen = true;
	}

	async function confirmDelete() {
		if (!deleteTarget) return;
		const template = deleteTarget;
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
		const cats: string[] = [];
		templates.forEach((t) => {
			if (t.category && !cats.includes(t.category)) cats.push(t.category);
		});
		return cats.sort();
	});

	let selectedCategory = $state<string | null>(null);

	// Clears filter when its category disappears
	$effect(() => {
		if (selectedCategory && !categories.includes(selectedCategory)) {
			selectedCategory = null;
		}
	});

	let filteredTemplates = $derived.by(() => {
		if (!selectedCategory) return templates;
		return templates.filter((t) => t.category === selectedCategory);
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<span class="tabular text-xs text-muted-foreground">
			{templates.length}
			{templates.length === 1 ? 'template' : 'templates'}
		</span>
		<div class="flex items-center gap-2">
			<Button
				variant="ghost"
				size="icon"
				class="size-8"
				onclick={() => loadTemplates()}
				disabled={loading}
				title="Refresh"
			>
				<RefreshCw class="size-4 {loading ? 'animate-spin' : ''}" />
			</Button>
			<Button size="sm" onclick={() => (createDialogOpen = true)}>
				<Plus class="size-4" />
				Create template
			</Button>
		</div>
	</div>

	{#if categories.length > 0}
		<div class="flex flex-wrap items-center gap-1">
			<Button
				variant={selectedCategory === null ? 'secondary' : 'ghost'}
				size="sm"
				class="h-8"
				onclick={() => (selectedCategory = null)}
			>
				All
			</Button>
			{#each categories as cat (cat)}
				<Button
					variant={selectedCategory === cat ? 'secondary' : 'ghost'}
					size="sm"
					class="h-8"
					onclick={() => (selectedCategory = cat)}
				>
					{cat}
				</Button>
			{/each}
		</div>
	{/if}

	{#if loading && templates.length === 0}
		<div class="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
			{#each Array(3) as _, i (i)}
				<Skeleton class="h-40 rounded-lg" />
			{/each}
		</div>
	{:else if templates.length === 0}
		<div class="rounded-lg border bg-card">
			<EmptyState
				icon={Layers}
				title="No templates found"
				description="You don't have any module templates configured yet."
			>
				<Button size="sm" onclick={() => (createDialogOpen = true)}>
					<Plus class="size-4" />
					Create template
				</Button>
			</EmptyState>
		</div>
	{:else if filteredTemplates.length === 0}
		<div class="rounded-lg border bg-card">
			<EmptyState
				icon={Search}
				title="No matching templates"
				description="No templates in this category anymore."
			>
				<Button variant="outline" size="sm" onclick={() => (selectedCategory = null)}>
					Clear filter
				</Button>
			</EmptyState>
		</div>
	{:else}
		<div class="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
			{#each filteredTemplates as template (template.id)}
				<div
					class="group flex flex-col rounded-lg border bg-card p-4 transition-colors hover:border-primary/20"
				>
					<div class="flex items-start gap-3">
						<div
							class="flex size-10 shrink-0 items-center justify-center rounded-lg border border-border/60 bg-muted/40 text-muted-foreground"
						>
							<DynamicIcon name={template.icon} class="size-5" fallback="Package" />
						</div>
						<div class="min-w-0 flex-1">
							<h3 class="truncate text-sm font-medium">{template.name}</h3>
							<div class="mt-1 flex flex-wrap items-center gap-1">
								{#if template.type === ModuleTemplateType.BUILTIN}
									<Badge variant="secondary">Built-in</Badge>
								{:else}
									<Badge variant="outline">Custom</Badge>
								{/if}
								{#if template.category}
									<Badge variant="secondary">{template.category}</Badge>
								{/if}
							</div>
						</div>
					</div>

					<p class="mt-3 line-clamp-2 flex-1 text-sm text-muted-foreground">
						{template.description || 'No description provided'}
					</p>

					<div class="mt-3 flex items-center justify-between gap-2 border-t pt-2.5">
						<div class="min-w-0 truncate font-mono text-xs text-muted-foreground">
							{template.dockerImage}
						</div>

						{#if template.type === ModuleTemplateType.CUSTOM}
							<div
								class="flex shrink-0 items-center gap-1 opacity-60 transition-opacity group-hover:opacity-100"
							>
								<Button
									size="icon"
									variant="ghost"
									class="size-7"
									onclick={() => openEditDialog(template)}
									title="Edit template"
								>
									<Settings class="size-3.5" />
								</Button>
								<Button
									size="icon"
									variant="ghost"
									class="size-7 text-status-danger hover:bg-status-danger/10 hover:text-status-danger"
									onclick={() => requestDelete(template)}
									title="Delete template"
								>
									<Trash2 class="size-3.5" />
								</Button>
							</div>
						{:else}
							<span class="shrink-0 text-xs text-muted-foreground">Read-only</span>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<ConfirmDialog
	bind:open={deleteOpen}
	title="Delete template {deleteTarget?.name ?? ''}?"
	description="This cannot be undone and will not affect existing instances."
	confirmLabel="Delete template"
	destructive
	onConfirm={confirmDelete}
/>

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
