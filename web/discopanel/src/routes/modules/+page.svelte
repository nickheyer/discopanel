<script lang="ts">
	import { Tabs, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { PageHeader } from '$lib/components/app';
	import TemplateManagement from './TemplateManagement.svelte';
	import ActiveModules from './ActiveModules.svelte';

	const TABS = [
		{ key: 'templates', label: 'Templates', desc: 'Reusable module definitions for any server' },
		{ key: 'active', label: 'Active instances', desc: 'Every module running across the panel' }
	] as const;

	let activeTab = $state<string>('templates');
</script>

<svelte:head>
	<title>Modules · DiscoPanel</title>
</svelte:head>

<div class="mx-auto w-full max-w-6xl space-y-5 p-4 sm:p-6 2xl:max-w-7xl">
	<PageHeader
		title="Modules"
		description={TABS.find((t) => t.key === activeTab)?.desc ?? 'Companion services for servers'}
	/>

	<Tabs bind:value={activeTab}>
		<div class="overflow-x-auto border-b">
			<TabsList class="h-auto w-max justify-start gap-1 bg-transparent p-0">
				{#each TABS as tab (tab.key)}
					<TabsTrigger
						value={tab.key}
						class="rounded-none border-0 border-b-2 border-transparent px-3 pt-1.5 pb-2 text-sm text-muted-foreground shadow-none data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:text-foreground data-[state=active]:shadow-none"
					>
						{tab.label}
					</TabsTrigger>
				{/each}
			</TabsList>
		</div>
	</Tabs>

	{#if activeTab === 'templates'}
		<TemplateManagement />
	{:else if activeTab === 'active'}
		<ActiveModules active={activeTab === 'active'} />
	{/if}
</div>
