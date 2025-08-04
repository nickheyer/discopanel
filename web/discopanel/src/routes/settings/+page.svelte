<script lang="ts">
	import { onMount } from 'svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { toast } from 'svelte-sonner';
	import type { ConfigCategory } from '$lib/api/types';
	
	let globalConfig = $state<ConfigCategory[]>([]);
	let loading = $state(true);
	let saving = $state(false);
	
	async function loadGlobalSettings() {
		loading = true;
		try {
			const response = await fetch('/api/v1/settings');
			if (!response.ok) throw new Error('Failed to load settings');
			globalConfig = await response.json();
		} catch (error) {
			toast.error('Failed to load global settings');
			console.error(error);
		} finally {
			loading = false;
		}
	}
	
	async function saveGlobalSettings(updates: Record<string, any>) {
		saving = true;
		try {
			const response = await fetch('/api/v1/settings', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(updates)
			});
			
			if (!response.ok) throw new Error('Failed to save settings');
			
			globalConfig = await response.json();
			toast.success('Global settings saved successfully');
		} catch (error) {
			toast.error('Failed to save global settings');
			console.error(error);
		} finally {
			saving = false;
		}
	}
	
	onMount(() => {
		loadGlobalSettings();
	});
</script>

<div class="flex-1 space-y-4 p-8">
	<div class="flex items-center justify-between">
		<h2 class="text-3xl font-bold tracking-tight">Global Settings</h2>
	</div>
	
	<Card>
		<CardHeader>
			<CardTitle>Default Server Configuration</CardTitle>
			<CardDescription>
				Configure default values that will be applied to all new servers. These settings can be overridden on a per-server basis.
			</CardDescription>
		</CardHeader>
		<CardContent>
			{#if loading}
				<div class="flex items-center justify-center py-8">
					<div class="text-muted-foreground">Loading settings...</div>
				</div>
			{:else}
				<ServerConfiguration 
					config={globalConfig} 
					onSave={saveGlobalSettings}
					{saving}
				/>
			{/if}
		</CardContent>
	</Card>
</div>