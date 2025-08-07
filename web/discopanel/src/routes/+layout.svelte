<script lang="ts">
	import '../app.css';
	import { ModeWatcher } from "mode-watcher";
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { SidebarProvider, SidebarInset, Sidebar, SidebarContent, SidebarGroup, SidebarGroupLabel, SidebarGroupContent, SidebarMenu, SidebarMenuItem, SidebarMenuButton, SidebarHeader, SidebarFooter, SidebarTrigger } from '$lib/components/ui/sidebar';
	import { Separator } from '$lib/components/ui/separator';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '$lib/components/ui/dropdown-menu';
	import { Avatar, AvatarFallback } from '$lib/components/ui/avatar';
	import { serversStore, runningServers } from '$lib/stores/servers';
	import { authStore, currentUser, isAdmin, authEnabled } from '$lib/stores/auth';
	import { onMount } from 'svelte';
	import { Toaster } from '$lib/components/ui/sonner';
	
	import { Server, Home, Settings, Package, User, Users, LogOut, Shield } from '@lucide/svelte';

	let { children } = $props();
	
	let servers = $derived($serversStore);
	let runningCount = $derived($runningServers.length);
	let user = $derived($currentUser);
	let isAuthEnabled = $derived($authEnabled);
	let isUserAdmin = $derived($isAdmin);
	let loading = $state(true);
	
	onMount(async () => {
		// Check auth status first
		const authStatus = await authStore.checkAuthStatus();
		
		// If on login page, don't redirect
		const currentPath = page.url.pathname as string;
		if (currentPath === '/login') {
			loading = false;
			return;
		}
		
		if (authStatus.enabled) {
			if (authStatus.first_user_setup) {
				goto('/login');
				return;
			}
			const isValid = await authStore.validateSession();
			if (!isValid) {
				goto('/login');
				return;
			}
		}
		
		// Fetch servers after auth check
		await serversStore.fetchServers();
		loading = false;
	});
	
	function getUserInitials(user: any) {
		if (!user) return '?';
		return user.username.substring(0, 2).toUpperCase();
	}
	
	async function handleLogout() {
		await authStore.logout();
	}
</script>

<svelte:head>
	<title>DiscoPanel - Minecraft Server Management</title>
</svelte:head>

<ModeWatcher />
<Toaster position="top-right" expand={true} richColors />

{#if page.url.pathname === '/login'}
	{@render children?.()}
{:else if loading}
	<div class="flex items-center justify-center min-h-screen">
		<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
	</div>
{:else}
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
							<SidebarMenuItem>
								<SidebarMenuButton isActive={page.url.pathname.startsWith('/modpacks')}>
									{#snippet child({ props })}
										<a href="/modpacks" {...props}>
											<Package class="h-4 w-4" />
											<span>Modpacks</span>
										</a>
									{/snippet}
								</SidebarMenuButton>
							</SidebarMenuItem>
							{#if isUserAdmin}
								<SidebarMenuItem>
									<SidebarMenuButton isActive={page.url.pathname === '/users'}>
										{#snippet child({ props })}
											<a href="/users" {...props}>
												<Users class="h-4 w-4" />
												<span>Users</span>
											</a>
										{/snippet}
									</SidebarMenuButton>
								</SidebarMenuItem>
							{/if}
							<SidebarMenuItem>
								<SidebarMenuButton isActive={page.url.pathname === '/settings'}>
									{#snippet child({ props })}
										<a href="/settings" {...props}>
											<Settings class="h-4 w-4" />
											<span>Settings</span>
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
				{#if user}
					<div class="px-2 py-2 border-t">
						<div class="flex items-center gap-2 text-sm">
							<User class="h-4 w-4" />
							<div class="flex-1 truncate">
								<p class="truncate font-medium">{user.username}</p>
								<p class="text-xs text-muted-foreground capitalize">{user.role}</p>
							</div>
							{#if user.role === 'admin'}
								<Shield class="h-4 w-4 text-primary" />
							{/if}
						</div>
					</div>
				{/if}
				<div class="px-2 py-1 text-xs text-muted-foreground">
					v0.0.1
				</div>
			</SidebarFooter>
		</Sidebar>
		
		<SidebarInset>
			<header class="flex h-16 items-center gap-2 border-b px-4">
				<SidebarTrigger />
				<Separator orientation="vertical" class="mr-2 h-4" />
				<div class="flex-1"></div>
				
				{#if isAuthEnabled && user}
					<DropdownMenu>
						<DropdownMenuTrigger>
							<Button variant="ghost" class="relative h-8 w-8 rounded-full">
								<Avatar class="h-8 w-8">
									<AvatarFallback>{getUserInitials(user)}</AvatarFallback>
								</Avatar>
							</Button>
						</DropdownMenuTrigger>
						<DropdownMenuContent class="w-56" align="end">
							<DropdownMenuLabel>
								<div class="flex flex-col space-y-1">
									<p class="text-sm font-medium leading-none">{user.username}</p>
									{#if user.email}
										<p class="text-xs leading-none text-muted-foreground">{user.email}</p>
									{/if}
									<p class="text-xs leading-none text-muted-foreground capitalize">Role: {user.role}</p>
								</div>
							</DropdownMenuLabel>
							<DropdownMenuSeparator />
							<DropdownMenuItem onclick={() => goto('/profile')}>
								<User class="mr-2 h-4 w-4" />
								<span>Profile</span>
							</DropdownMenuItem>
							{#if isUserAdmin}
								<DropdownMenuSeparator />
								<DropdownMenuItem onclick={() => goto('/users')}>
									<Users class="mr-2 h-4 w-4" />
									<span>Manage Users</span>
								</DropdownMenuItem>
							{/if}
							<DropdownMenuSeparator />
							<DropdownMenuItem onclick={handleLogout}>
								<LogOut class="mr-2 h-4 w-4" />
								<span>Log out</span>
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				{/if}
			</header>
			<main class="flex-1">
				{@render children?.()}
			</main>
		</SidebarInset>
	</SidebarProvider>
{/if}