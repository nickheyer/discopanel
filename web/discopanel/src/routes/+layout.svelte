<script lang="ts">
	import '../app.css';
	import { ModeWatcher } from 'mode-watcher';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import {
		SidebarProvider,
		SidebarInset,
		Sidebar,
		SidebarContent,
		SidebarGroup,
		SidebarGroupLabel,
		SidebarGroupContent,
		SidebarMenu,
		SidebarMenuItem,
		SidebarMenuButton,
		SidebarHeader,
		SidebarFooter,
		SidebarTrigger
	} from '$lib/components/ui/sidebar';
	import { Separator } from '$lib/components/ui/separator';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Avatar, AvatarFallback } from '$lib/components/ui/avatar';
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger
	} from '$lib/components/ui/dropdown-menu';
	import { serversStore, runningServers } from '$lib/stores/servers';
	import { authStore, currentUser, isAdmin } from '$lib/stores/auth';
	import { onMount } from 'svelte';
	import { Toaster } from '$lib/components/ui/sonner';

	import { Server, Home, Settings, Package, User, Users, Shield, LogOut } from '@lucide/svelte';

	let { children } = $props();

	let servers = $derived($serversStore);
	let runningCount = $derived($runningServers.length);
	let user = $derived($currentUser);
	let isUserAdmin = $derived($isAdmin);
	let loading = $state(true);
	let isAuthEnabled = $derived($authStore.authEnabled);

	function getUserInitials(user: any) {
		if (!user) return '';
		return user.username.slice(0, 2).toUpperCase();
	}

	async function handleLogout() {
		await authStore.logout();
	}

	onMount(async () => {
		const authStatus = await authStore.checkAuthStatus();
		loading = false;
		if (authStatus.enabled) {
			if (authStatus.firstUserSetup) {
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
	});

</script>

<svelte:head>
	<title>DiscoPanel - Minecraft Server Management</title>
</svelte:head>

<ModeWatcher />
<Toaster position="top-right" expand={true} richColors />

{#if page.url.pathname === '/login'}
	{@render children?.()}
{:else if loading}
	<div class="flex min-h-screen items-center justify-center">
		<div class="border-primary h-8 w-8 animate-spin rounded-full border-b-2"></div>
	</div>
{:else}
	<div>
		<SidebarProvider>
			<Sidebar collapsible="icon">
				<SidebarHeader class="my-2">
					<div class="m-auto flex items-center gap-2">
						<img src="/g1_24x24.png" alt="DiscoPanel Logo" class="h-6 w-6" />
						<span class="text-lg font-bold group-data-[collapsible=icon]:hidden">DiscoPanel</span>
					</div>
				</SidebarHeader>

				<SidebarContent>
					<SidebarGroup>
						<SidebarGroupLabel class="group-data-[collapsible=icon]:opacity-0">Navigation</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								<SidebarMenuItem>
									<SidebarMenuButton isActive={page.url.pathname === '/'}>
										{#snippet child({ props })}
											<a href="/" {...props}>
												<Home class="h-4 w-4" />
												<span class="group-data-[collapsible=icon]:hidden">Dashboard</span>
											</a>
										{/snippet}
									</SidebarMenuButton>
								</SidebarMenuItem>
								<SidebarMenuItem>
									<SidebarMenuButton isActive={page.url.pathname.startsWith('/servers')}>
										{#snippet child({ props })}
											<a href="/servers" {...props}>
												<Server class="h-4 w-4" />
												<span class="group-data-[collapsible=icon]:hidden">Servers</span>
												{#if runningCount > 0}
													<Badge variant="secondary" class="ml-auto group-data-[collapsible=icon]:hidden">{runningCount}</Badge>
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
												<span class="group-data-[collapsible=icon]:hidden">Modpacks</span>
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
													<span class="group-data-[collapsible=icon]:hidden">Users</span>
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
												<span class="group-data-[collapsible=icon]:hidden">Settings</span>
											</a>
										{/snippet}
									</SidebarMenuButton>
								</SidebarMenuItem>
							</SidebarMenu>
						</SidebarGroupContent>
					</SidebarGroup>

					{#if servers.length > 0}
						<div class="group-data-[collapsible=icon]:hidden">
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
															<div class="flex w-full items-center gap-2">
																<div
																	class="h-2 w-2 rounded-full {server.status === 'running'
																		? 'bg-green-500'
																		: server.status === 'starting' || server.status === 'stopping'
																			? 'bg-yellow-500'
																			: 'bg-gray-400'}"
																></div>
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
						</div>
					{/if}
				</SidebarContent>

				<SidebarFooter>
					<Separator orientation="horizontal" />
					<div class="flex items-center justify-between">
						{#if isAuthEnabled && user}
							<DropdownMenu>
								<div class="py-2 w-full">
									<DropdownMenuTrigger class="w-full h-full justify-start group-data-[collapsible=icon]:p-0">
										{#snippet child({ props })}
											<Button {...props} variant="ghost">
												<Avatar class="h-8 w-8">
													<AvatarFallback>{getUserInitials(user)}</AvatarFallback>
												</Avatar>
												<div class="ml-2 flex-1 text-left group-data-[collapsible=icon]:hidden">
													<p class="text-sm font-medium leading-none">{user.username}</p>
													<p class="text-xs text-muted-foreground capitalize">{user.role}</p>
												</div>
											</Button>
										{/snippet}
									</DropdownMenuTrigger>
								</div>
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

					</div>
					<Separator orientation="horizontal" class="mb-2" />
					<div class="ml-auto flex items-center gap-2">
						<span class="text-muted-foreground text-xs group-data-[collapsible=icon]:hidden">v0.0.1</span>
						<SidebarTrigger />
					</div>
				</SidebarFooter>
			</Sidebar>

			<SidebarInset class="flex h-screen flex-col">
				<main class="flex-1">
					{@render children?.()}
				</main>
			</SidebarInset>
		</SidebarProvider>
	</div>
{/if}
