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
	<div class="flex flex-wrap gap-2 mb-6">
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
			class="w-full flex items-center gap-5 p-5 rounded-xl border bg-card text-left hover:bg-muted/50 hover:border-primary/50 transition-all group cursor-pointer"
			role="button"
			onclick={() => onSelect(template)}
		>
			<div class="h-14 w-14 rounded-xl bg-primary/10 flex items-center justify-center shrink-0">
				<DynamicIcon name={template.icon} class="h-7 w-7 text-primary" fallback="Package" />
			</div>
			<div class="flex-1 min-w-0 space-y-1">
				<div class="flex items-center gap-3">
					<span class="font-semibold text-base">{template.name}</span>
					{#if template.category}
						<Badge variant="secondary" class="text-xs">{template.category}</Badge>
					{/if}
				</div>
				<p class="text-sm text-muted-foreground leading-relaxed">
					{template.description || 'No description provided'}
				</p>
			</div>
			
			<div class="flex items-center gap-2 shrink-0">
				{#if template.type === ModuleTemplateType.CUSTOM && onDelete}
					<Button
						variant="ghost"
						size="icon"
						class="opacity-0 group-hover:opacity-100 transition-opacity text-destructive hover:text-destructive hover:bg-destructive/10"
						onclick={(e) => {
							e.stopPropagation();
							onDelete?.(template);
						}}
					>
						<Trash2 class="h-5 w-5" />
					</Button>
				{/if}
				<ChevronRight class="h-5 w-5 text-muted-foreground group-hover:text-primary transition-colors" />
			</div>
		</div>
	{/each}
</div>

{#if !templates || templates.length === 0}
	<div class="flex flex-col items-center justify-center py-20 text-center">
		<div class="h-20 w-20 rounded-2xl bg-muted flex items-center justify-center mb-6">
			<Package class="h-10 w-10 text-muted-foreground/50" />
		</div>
		<h3 class="text-lg font-medium mb-2">No templates available</h3>
		<p class="text-sm text-muted-foreground max-w-sm">
			Create a custom template to get started with your module configuration
		</p>
	</div>
{/if}
