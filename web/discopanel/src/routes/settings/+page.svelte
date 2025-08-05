<script lang="ts">
	import { onMount } from 'svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { toast } from 'svelte-sonner';
	import { Settings } from '@lucide/svelte';
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

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-gradient-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<Settings class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Global Settings</h2>
				<p class="text-base text-muted-foreground">Configure default values for all new servers</p>
			</div>
		</div>
	</div>
	
	<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
		<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
		<CardHeader class="relative pb-6">
			<CardTitle class="text-2xl font-semibold">Default Server Configuration</CardTitle>
			<CardDescription class="text-base">
				Configure default values that will be applied to all new servers. These settings can be overridden on a per-server basis.
			</CardDescription>
		</CardHeader>
		<CardContent class="relative">
			{#if loading}
				<div class="flex items-center justify-center py-16">
					<div class="text-center space-y-3">
						<div class="h-12 w-12 mx-auto rounded-full bg-primary/10 flex items-center justify-center animate-pulse">
							<Settings class="h-6 w-6 text-primary" />
						</div>
						<div class="text-muted-foreground font-medium">Loading settings...</div>
					</div>
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

<ScrollToTop />