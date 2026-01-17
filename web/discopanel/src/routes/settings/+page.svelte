<script lang="ts">
	import { onMount } from 'svelte';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import { Settings, Globe, Server, Shield, HelpCircle, ScrollText } from '@lucide/svelte';
	import type { ConfigCategory } from '$lib/proto/discopanel/v1/config_pb';
	import { rpcClient } from '$lib/api/rpc-client';
	import RoutingSettings from '$lib/components/routing-settings.svelte';
	import AuthSettings from '$lib/components/auth-settings.svelte';
	import SupportSettings from '$lib/components/support-settings.svelte';
	import LogsSettings from '$lib/components/logs-settings.svelte';
	
	let globalConfig = $state<ConfigCategory[]>([]);
	let loading = $state(true);
	let saving = $state(false);
	let activeTab = $state('server-config');
	
	async function loadGlobalSettings() {
		loading = true;
		try {
			const response = await rpcClient.config.getGlobalSettings({});
			globalConfig = response.categories;
		} catch (error) {
			toast.error('Failed to load global settings');
			console.error(error);
		} finally {
			loading = false;
		}
	}
	
	async function saveGlobalSettings(updates: Record<string, string>) {
		saving = true;
		try {
			const response = await rpcClient.config.updateGlobalSettings({
				updates
			});

			globalConfig = response.categories;
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
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Settings</h2>
				<p class="text-base text-muted-foreground">Configure DiscoPanel and default server settings</p>
			</div>
		</div>
	</div>
	
	<Tabs value={activeTab} onValueChange={(v) => activeTab = v || 'server-config'} class="space-y-6">
		<TabsList class="grid w-full max-w-3xl grid-cols-5">
			<TabsTrigger value="server-config" class="flex items-center gap-2">
				<Server class="h-4 w-4" />
				Server Defaults
			</TabsTrigger>
			<TabsTrigger value="routing" class="flex items-center gap-2">
				<Globe class="h-4 w-4" />
				Routing
			</TabsTrigger>
			<TabsTrigger value="auth" class="flex items-center gap-2">
				<Shield class="h-4 w-4" />
				Authentication
			</TabsTrigger>
			<TabsTrigger value="logs" class="flex items-center gap-2">
				<ScrollText class="h-4 w-4" />
				Logs
			</TabsTrigger>
			<TabsTrigger value="support" class="flex items-center gap-2">
				<HelpCircle class="h-4 w-4" />
				Support
			</TabsTrigger>
		</TabsList>
		
		<TabsContent value="server-config" class="space-y-4">
			{#if loading}
				<Card>
					<CardContent class="py-16">
						<div class="flex items-center justify-center">
							<div class="text-center space-y-3">
								<div class="h-12 w-12 mx-auto rounded-full bg-primary/10 flex items-center justify-center animate-pulse">
									<Settings class="h-6 w-6 text-primary" />
								</div>
								<div class="text-muted-foreground font-medium">Loading settings...</div>
							</div>
						</div>
					</CardContent>
				</Card>
			{:else}
				<ServerConfiguration
					config={globalConfig}
					onSave={saveGlobalSettings}
					{saving}
				/>
			{/if}
		</TabsContent>
		
		<TabsContent value="routing" class="space-y-4">
			<RoutingSettings />
		</TabsContent>
		
		<TabsContent value="auth" class="space-y-4">
			<AuthSettings />
		</TabsContent>

		<TabsContent value="logs" class="space-y-4">
			<LogsSettings />
		</TabsContent>

		<TabsContent value="support" class="space-y-4">
			<SupportSettings />
		</TabsContent>
	</Tabs>
</div>

<ScrollToTop />