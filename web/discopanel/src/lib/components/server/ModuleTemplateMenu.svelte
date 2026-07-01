<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import DynamicIcon from '$lib/components/ui/DynamicIcon.svelte';
	import { Package, ChevronRight, Trash2 } from '@lucide/svelte';
	import { ModuleTemplateType, type ModuleTemplate } from '$lib/proto/discopanel/v1/module_pb';

	interface Props {
		templates?: ModuleTemplate[];
		onSelect: (template: ModuleTemplate) => void;
		onDelete?: (template: ModuleTemplate) => void;
	}

	let { templates, onSelect, onDelete }: Props = $props();

	let selectedCategory = $state<string | null>(null);

	let categories = $derived.by(() => {
		if (!templates) return [];
		const cats = new Set<string>(); // eslint-disable-line svelte/prefer-svelte-reactivity
		templates.forEach((t) => {
			if (t.category) cats.add(t.category);
		});
		return Array.from(cats).sort();
	});

	let filteredTemplates = $derived.by(() => {
		if (!templates) return [];
		if (!selectedCategory) return templates;
		return templates.filter((t) => t.category === selectedCategory);
	});
</script>

{#if categories.length > 0}
	<div class="mb-6 flex flex-wrap gap-2">
		<Button
			variant={selectedCategory === null ? 'default' : 'outline'}
			size="sm"
			onclick={() => (selectedCategory = null)}
			class="h-9 px-4"
		>
			All
		</Button>
		{#each categories as cat (cat)}
			<Button
				variant={selectedCategory === cat ? 'default' : 'outline'}
				size="sm"
				onclick={() => (selectedCategory = cat)}
				class="h-9 px-4"
			>
				{cat}
			</Button>
		{/each}
	</div>
{/if}

<div class="space-y-3">
	{#each filteredTemplates as template (template.name)}
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<!-- svelte-ignore a11y_interactive_supports_focus -->
		<div
			class="group flex w-full cursor-pointer items-center gap-5 rounded-xl border bg-card p-5 text-left transition-all hover:border-primary/50 hover:bg-muted/50"
			role="button"
			onclick={() => onSelect(template)}
		>
			<div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-primary/10">
				<DynamicIcon name={template.icon} class="h-7 w-7 text-primary" fallback="Package" />
			</div>
			<div class="min-w-0 flex-1 space-y-1">
				<div class="flex items-center gap-3">
					<span class="text-base font-semibold">{template.name}</span>
					{#if template.category}
						<Badge variant="secondary" class="text-xs">{template.category}</Badge>
					{/if}
				</div>
				<p class="text-sm leading-relaxed text-muted-foreground">
					{template.description || 'No description provided'}
				</p>
			</div>

			<div class="flex shrink-0 items-center gap-2">
				{#if template.type === ModuleTemplateType.CUSTOM && onDelete}
					<Button
						variant="ghost"
						size="icon"
						class="text-destructive opacity-0 transition-opacity group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive"
						onclick={(e) => {
							e.stopPropagation();
							onDelete?.(template);
						}}
					>
						<Trash2 class="h-5 w-5" />
					</Button>
				{/if}
				<ChevronRight
					class="h-5 w-5 text-muted-foreground transition-colors group-hover:text-primary"
				/>
			</div>
		</div>
	{/each}
</div>

{#if !templates || templates.length === 0}
	<div class="flex flex-col items-center justify-center py-20 text-center">
		<div class="mb-6 flex h-20 w-20 items-center justify-center rounded-2xl bg-muted">
			<Package class="h-10 w-10 text-muted-foreground/50" />
		</div>
		<h3 class="mb-2 text-lg font-medium">No templates available</h3>
		<p class="max-w-sm text-sm text-muted-foreground">
			Create a custom template to get started with your module configuration
		</p>
	</div>
{/if}
