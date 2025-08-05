<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Progress } from '$lib/components/ui/progress';
	import { serversStore } from '$lib/stores/servers';
	import { Server, Cpu, MemoryStick, HardDrive, Activity, Plus, LayoutDashboard } from '@lucide/svelte';
	import type { Server as ServerType } from '$lib/api/types';

	let servers = $state<ServerType[]>([]);
	let stats = $state({
		total: 0,
		running: 0,
		stopped: 0,
		totalMemory: 0,
		usedMemory: 0
	});

	onMount(() => {
		const unsubscribe = serversStore.subscribe(value => {
			servers = value;
			stats = {
				total: value.length,
				running: value.filter(s => s.status === 'running').length,
				stopped: value.filter(s => s.status === 'stopped').length,
				totalMemory: value.reduce((acc, s) => acc + s.memory, 0),
				usedMemory: value.filter(s => s.status === 'running').reduce((acc, s) => acc + s.memory, 0)
			};
		});

		return unsubscribe;
	});

	const getStatusColor = (status: ServerType['status']) => {
		switch (status) {
			case 'running':
				return 'text-green-500';
			case 'starting':
			case 'stopping':
				return 'text-yellow-500';
			case 'stopped':
				return 'text-gray-400';
			case 'error':
				return 'text-red-500';
			default:
				return 'text-gray-400';
		}
	};

	const getStatusText = (status: ServerType['status']) => {
		return status.charAt(0).toUpperCase() + status.slice(1);
	};
</script>

<div class="flex-1 space-y-8 h-full p-8 pt-6 bg-gradient-to-br from-background to-muted/10">
	<div class="flex items-center justify-between pb-6 border-b-2 border-border/50">
		<div class="flex items-center gap-4">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center shadow-lg">
				<LayoutDashboard class="h-8 w-8 text-primary" />
			</div>
			<div class="space-y-1">
				<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">Dashboard</h2>
				<p class="text-base text-muted-foreground">Monitor and manage your Minecraft server infrastructure</p>
			</div>
		</div>
		<div class="flex items-center gap-2">
			<Button href="/servers/new" size="default" class="bg-gradient-to-r from-primary to-primary/80 hover:from-primary/90 hover:to-primary/70 shadow-lg hover:shadow-xl hover:scale-[1.02] transition-all">
				<Plus class="h-5 w-5 mr-2" />
				New Server
			</Button>
		</div>
	</div>

	<div class="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
		<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
				<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Total Servers</CardTitle>
				<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
					<Server class="h-6 w-6 text-primary" />
				</div>
			</CardHeader>
			<CardContent class="pt-2">
				<div class="text-3xl font-bold">{stats.total}</div>
				<p class="text-sm text-muted-foreground mt-1">
					{stats.running} running, {stats.stopped} stopped
				</p>
			</CardContent>
		</Card>

		<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
				<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Active Servers</CardTitle>
				<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
					<Activity class="h-6 w-6 text-primary" />
				</div>
			</CardHeader>
			<CardContent class="pt-2">
				<div class="text-3xl font-bold text-green-600">{stats.running}</div>
				<Progress value={(stats.running / Math.max(stats.total, 1)) * 100} class="mt-3 h-3" />
			</CardContent>
		</Card>

		<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
				<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">Memory Usage</CardTitle>
				<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
					<MemoryStick class="h-6 w-6 text-primary" />
				</div>
			</CardHeader>
			<CardContent class="pt-2">
				<div class="flex items-baseline gap-2">
					<span class="text-3xl font-bold">{(stats.usedMemory / 1024).toFixed(1)}</span>
					<span class="text-sm text-muted-foreground font-medium">GB</span>
				</div>
				<p class="text-sm text-muted-foreground mt-1">
					of {(stats.totalMemory / 1024).toFixed(1)} GB allocated
				</p>
			</CardContent>
		</Card>

		<Card class="group relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="flex flex-row items-center justify-between space-y-0 pb-3">
				<CardTitle class="text-sm font-semibold text-muted-foreground uppercase tracking-wider">CPU Load</CardTitle>
				<div class="h-12 w-12 rounded-xl bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center group-hover:scale-110 transition-transform">
					<Cpu class="h-6 w-6 text-primary" />
				</div>
			</CardHeader>
			<CardContent class="pt-2">
				<div class="text-3xl font-bold text-muted-foreground">N/A</div>
				<p class="text-sm text-muted-foreground mt-1">
					System monitoring coming soon
				</p>
			</CardContent>
		</Card>
	</div>

	<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-7">
		<Card class="col-span-4">
			<CardHeader>
				<CardTitle>Recent Servers</CardTitle>
				<CardDescription>
					Your recently accessed Minecraft servers
				</CardDescription>
			</CardHeader>
			<CardContent>
				{#if servers.length === 0}
					<div class="text-center py-8">
						<Server class="mx-auto h-12 w-12 text-muted-foreground" />
						<h3 class="mt-2 text-sm font-semibold">No servers</h3>
						<p class="mt-1 text-sm text-muted-foreground">Get started by creating a new server.</p>
						<div class="mt-6">
							<Button href="/servers/new">
								<Plus class="h-4 w-4 mr-2" />
								New Server
							</Button>
						</div>
					</div>
				{:else}
					<div class="space-y-4">
						{#each servers.slice(0, 5) as server}
							<div class="flex items-center">
								<div class="flex items-center space-x-4 flex-1">
									<div class="h-2 w-2 rounded-full {getStatusColor(server.status).replace('text-', 'bg-')}"></div>
									<div class="flex-1 space-y-1">
										<p class="text-sm font-medium leading-none">
											{server.name}
										</p>
										<p class="text-sm text-muted-foreground">
											{server.mc_version} • {server.mod_loader}
										</p>
									</div>
									<div class="text-sm {getStatusColor(server.status)}">
										{getStatusText(server.status)}
									</div>
								</div>
								<Button variant="ghost" size="sm" href="/servers/{server.id}">
									Manage
								</Button>
							</div>
						{/each}
					</div>
				{/if}
			</CardContent>
		</Card>

		<Card class="col-span-3">
			<CardHeader>
				<CardTitle>System Overview</CardTitle>
				<CardDescription>
					System resource utilization
				</CardDescription>
			</CardHeader>
			<CardContent>
				<div class="space-y-4">
					<div>
						<div class="flex items-center justify-between text-sm">
							<span>CPU Usage</span>
							<span class="text-muted-foreground">N/A</span>
						</div>
						<Progress value={0} class="mt-2" />
					</div>
					<div>
						<div class="flex items-center justify-between text-sm">
							<span>Memory</span>
							<span class="text-muted-foreground">{stats.usedMemory} / {stats.totalMemory} MB</span>
						</div>
						<Progress value={(stats.usedMemory / Math.max(stats.totalMemory, 1)) * 100} class="mt-2" />
					</div>
					<div>
						<div class="flex items-center justify-between text-sm">
							<span>Storage</span>
							<span class="text-muted-foreground">N/A</span>
						</div>
						<Progress value={0} class="mt-2" />
					</div>
					<div>
						<div class="flex items-center justify-between text-sm">
							<span>Network I/O</span>
							<span class="text-muted-foreground">N/A</span>
						</div>
						<Progress value={0} class="mt-2" />
					</div>
				</div>
			</CardContent>
		</Card>
	</div>
</div>
