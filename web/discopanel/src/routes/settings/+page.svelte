<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import ServerConfiguration from '$lib/components/server-configuration.svelte';
	import ScrollToTop from '$lib/components/scroll-to-top.svelte';
	import {
		Card,
		CardContent,
		CardDescription,
		CardHeader,
		CardTitle
	} from '$lib/components/ui/card';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import { Settings, Globe, Server, Shield, HelpCircle } from '@lucide/svelte';
	import type { ConfigCategory } from '$lib/api/types';
	import RoutingSettings from '$lib/components/routing-settings.svelte';
	import AuthSettings from '$lib/components/auth-settings.svelte';
	import SupportSettings from '$lib/components/support-settings.svelte';
	import { authStore, authEnabled, isAdmin } from '$lib/stores/auth';

	let globalConfig = $state<ConfigCategory[]>([]);
	let loading = $state(true);
	let saving = $state(false);
	let activeTab = $state('server-config');
	let userIsAdmin = $derived($isAdmin);
	let isAuthEnabled = $derived($authStore.authEnabled);

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
		// Redirect non-admin users
		if (isAuthEnabled && !userIsAdmin) {
			toast.error('Access denied. Admin privileges required.');
			goto('/');
			return;
		}
		loadGlobalSettings();
	});
</script>

<div class="from-background to-muted/10 h-full flex-1 space-y-8 bg-gradient-to-br p-8 pt-6">
	<div class="border-border/50 flex items-center justify-between border-b-2 pb-6">
		<div class="flex items-center gap-4">
			<div
				class="from-primary/20 to-primary/10 flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br shadow-lg"
			>
				<Settings class="text-primary h-8 w-8" />
			</div>
			<div class="space-y-1">
				<h2
					class="from-foreground to-foreground/70 bg-gradient-to-r bg-clip-text text-4xl font-bold tracking-tight text-transparent"
				>
					Settings
				</h2>
				<p class="text-muted-foreground text-base">
					Configure DiscoPanel and default server settings
				</p>
			</div>
		</div>
	</div>

	<Tabs
		value={activeTab}
		onValueChange={(v) => (activeTab = v || 'server-config')}
		class="space-y-6"
	>
		<TabsList class="grid w-full max-w-2xl grid-cols-4">
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
			<TabsTrigger value="support" class="flex items-center gap-2">
				<HelpCircle class="h-4 w-4" />
				Support
			</TabsTrigger>
		</TabsList>

		<TabsContent value="server-config" class="space-y-4">
			<Card
				class="hover:border-primary/50 from-card to-card/80 relative overflow-hidden border-2 bg-gradient-to-br transition-all duration-300 hover:shadow-2xl"
			>
				<div
					class="from-primary/10 absolute inset-0 bg-gradient-to-br via-transparent to-transparent opacity-0 transition-opacity duration-300 hover:opacity-100"
				></div>
				<CardHeader class="relative pb-6">
					<CardTitle class="text-2xl font-semibold">Default Server Configuration</CardTitle>
					<CardDescription class="text-base">
						Configure default values that will be applied to all new servers. These settings can be
						overridden on a per-server basis.
					</CardDescription>
				</CardHeader>
				<CardContent class="relative">
					{#if loading}
						<div class="flex items-center justify-center py-16">
							<div class="space-y-3 text-center">
								<div
									class="bg-primary/10 mx-auto flex h-12 w-12 animate-pulse items-center justify-center rounded-full"
								>
									<Settings class="text-primary h-6 w-6" />
								</div>
								<div class="text-muted-foreground font-medium">Loading settings...</div>
							</div>
						</div>
					{:else}
						<ServerConfiguration config={globalConfig} onSave={saveGlobalSettings} {saving} />
					{/if}
				</CardContent>
			</Card>
		</TabsContent>

		<TabsContent value="routing" class="space-y-4">
			<RoutingSettings />
		</TabsContent>

		<TabsContent value="auth" class="space-y-4">
			<AuthSettings />
		</TabsContent>

		<TabsContent value="support" class="space-y-4">
			<SupportSettings />
		</TabsContent>
	</Tabs>
</div>

<ScrollToTop />
