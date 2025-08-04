<script lang="ts">
	import '../app.css';
	import { ModeWatcher } from "mode-watcher";
	import { page } from '$app/state';
	import { SidebarProvider, SidebarInset, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarGroupContent, SidebarMenu, SidebarMenuItem, SidebarMenuButton, SidebarHeader, SidebarFooter, SidebarTrigger } from '$lib/components/ui/sidebar';
	import { Separator } from '$lib/components/ui/separator';
	import { Badge } from '$lib/components/ui/badge';
	import { serversStore, runningServers } from '$lib/stores/servers';
	import { onMount } from 'svelte';
	
	import { Server, Home } from '@lucide/svelte';

	let { children } = $props();
	
	let servers = $state<any[]>([]);
	let runningCount = $state(0);
	
	onMount(async () => {
		await serversStore.fetchServers();
	});
	
	$effect(() => {
		const unsubServers = serversStore.subscribe(value => servers = value);
		const unsubRunning = runningServers.subscribe(value => runningCount = value.length);
		
		return () => {
			unsubServers();
			unsubRunning();
		};
	});
</script>

<svelte:head>
	<title>DiscoPanel - Minecraft Server Management</title>
</svelte:head>

<ModeWatcher />

<SidebarProvider>
	<Sidebar>
		<SidebarHeader class="pl-0">
			<div class="flex items-center gap-2 m-auto">
				<img src="/g1_24x24.png" alt="DiscoPanel Logo" class="h-6 w-6" />
				<span class="text-lg font-bold">DiscoPanel</span>
			</div>
		</SidebarHeader>
		
		<SidebarContent>
			<SidebarGroup>
				<SidebarGroupLabel>Navigation</SidebarGroupLabel>
				<SidebarGroupContent>
					<SidebarMenu>
						<SidebarMenuItem>
							<SidebarMenuButton isActive={page.url.pathname === '/'}>
								{#snippet child({ props })}
									<a href="/" {...props}>
										<Home class="h-4 w-4" />
										<span>Dashboard</span>
									</a>
								{/snippet}
							</SidebarMenuButton>
						</SidebarMenuItem>
						<SidebarMenuItem>
							<SidebarMenuButton isActive={page.url.pathname.startsWith('/servers')}>
								{#snippet child({ props })}
									<a href="/servers" {...props}>
										<Server class="h-4 w-4" />
										<span>Servers</span>
										{#if runningCount > 0}
											<Badge variant="secondary" class="ml-auto">{runningCount}</Badge>
										{/if}
									</a>
								{/snippet}
							</SidebarMenuButton>
						</SidebarMenuItem>
					</SidebarMenu>
				</SidebarGroupContent>
			</SidebarGroup>
			
			{#if servers.length > 0}
				<Separator />
				<SidebarGroup>
					<SidebarGroupLabel>Quick Access</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{#each servers.slice(0, 5) as server}
								<SidebarMenuItem>
									<SidebarMenuButton isActive={page.url.pathname === `/servers/${server.id}`}>
										{#snippet child({ props })}
											<a href="/servers/{server.id}" {...props}>
												<div class="flex items-center gap-2 w-full">
													<div class="h-2 w-2 rounded-full {server.status === 'running' ? 'bg-green-500' : server.status === 'starting' || server.status === 'stopping' ? 'bg-yellow-500' : 'bg-gray-400'}"></div>
													<span class="truncate">{server.name}</span>
												</div>
											</a>
										{/snippet}
									</SidebarMenuButton>
								</SidebarMenuItem>
							{/each}
						</SidebarMenu>
					</SidebarGroupContent>
			</SidebarGroup>
			{/if}
		</SidebarContent>
		
		<SidebarFooter>
			<div class="px-2 py-1 text-xs text-muted-foreground">
				v0.0.1
			</div>
		</SidebarFooter>
	</Sidebar>
	
	<SidebarInset>
		<header class="flex h-16 items-center gap-2 border-b px-4">
			<SidebarTrigger />
			<Separator orientation="vertical" class="mr-2 h-4" />
		</header>
		<main class="flex-1">
			{@render children?.()}
		</main>
	</SidebarInset>
</SidebarProvider>
