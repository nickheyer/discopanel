<script lang="ts">
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { Puzzle, Server, Layers } from '@lucide/svelte';
	import TemplateManagement from './TemplateManagement.svelte';
	import ActiveModules from './ActiveModules.svelte';

	let activeTab = $state('templates');
</script>

<svelte:head>
	<title>Modules - DiscoPanel</title>
</svelte:head>

<div class="h-full flex-1 space-y-8 bg-linear-to-br from-background to-muted/10 p-8 pt-6">
	<div class="flex items-center justify-between border-b-2 border-border/50 pb-6">
		<div class="flex items-center gap-4">
			<div
				class="flex h-16 w-16 items-center justify-center rounded-2xl bg-linear-to-br from-primary/20 to-primary/10 shadow-lg"
			>
				<Puzzle class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2
					class="bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-4xl font-bold tracking-tight text-transparent"
				>
					Modules
				</h2>
				<p class="text-base text-muted-foreground">
					Manage global templates and view active instances
				</p>
			</div>
		</div>
	</div>

	<Tabs value={activeTab} onValueChange={(v) => (activeTab = v || activeTab)} class="space-y-6">
		<TabsList class="flex w-fit gap-1">
			<TabsTrigger value="templates" class="flex items-center gap-2 px-4">
				<Layers class="h-4 w-4" />
				Templates
			</TabsTrigger>
			<TabsTrigger value="active" class="flex items-center gap-2 px-4">
				<Server class="h-4 w-4" />
				Active Instances
			</TabsTrigger>
		</TabsList>

		<TabsContent value="templates" class="space-y-4">
			<TemplateManagement />
		</TabsContent>

		<TabsContent value="active" class="space-y-4">
			<ActiveModules />
		</TabsContent>
	</Tabs>
</div>
