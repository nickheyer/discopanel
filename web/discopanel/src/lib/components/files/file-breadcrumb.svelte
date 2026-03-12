<script lang="ts">
	import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '$lib/components/ui/breadcrumb';
	import { FolderRoot } from '@lucide/svelte';

	interface Props {
		currentPath: string;
		onNavigate: (path: string) => void;
	}

	let { currentPath, onNavigate }: Props = $props();

	let segments = $derived.by(() => {
		if (!currentPath) return [];
		return currentPath.split('/').filter(Boolean);
	});
</script>

<div class="flex items-center px-3 py-1 border-b text-xs bg-muted/10">
	<Breadcrumb>
		<BreadcrumbList>
			<BreadcrumbItem>
				{#if segments.length === 0}
					<BreadcrumbPage class="flex items-center gap-1 text-xs">
						<FolderRoot class="h-3 w-3" />
						Root
					</BreadcrumbPage>
				{:else}
					<BreadcrumbLink class="flex items-center gap-1 text-xs cursor-pointer" onclick={() => onNavigate('')}>
						<FolderRoot class="h-3 w-3" />
						Root
					</BreadcrumbLink>
				{/if}
			</BreadcrumbItem>
			{#each segments as segment, i}
				<BreadcrumbSeparator />
				<BreadcrumbItem>
					{#if i === segments.length - 1}
						<BreadcrumbPage class="text-xs">{segment}</BreadcrumbPage>
					{:else}
						<BreadcrumbLink
							class="text-xs cursor-pointer"
							onclick={() => onNavigate(segments.slice(0, i + 1).join('/'))}
						>
							{segment}
						</BreadcrumbLink>
					{/if}
				</BreadcrumbItem>
			{/each}
		</BreadcrumbList>
	</Breadcrumb>
</div>
