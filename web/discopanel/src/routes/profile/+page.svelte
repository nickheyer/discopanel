<script lang="ts">
	import { authStore, currentUser } from '$lib/stores/auth';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { User, Key, Loader2, Mail, Calendar, Clock, Shield, Activity } from '@lucide/svelte';
	import { getRoleBadgeVariant } from '$lib/utils/role-colors';

	let user = $derived($currentUser);
	let passwordForm = $state({
		oldPassword: '',
		newPassword: '',
		confirmPassword: ''
	});
	let saving = $state(false);

	let initials = $derived(
		user?.username
			? user.username
					.split(/[\s_-]+/)
					.slice(0, 2)
					.map((w) => w[0]?.toUpperCase() ?? '')
					.join('')
			: '?'
	);

	let primaryRole = $derived(user?.roles?.[0] ?? 'user');

	let memberSince = $derived(
		user?.createdAt
			? new Date(Number(user.createdAt.seconds) * 1000).toLocaleDateString(undefined, {
					year: 'numeric',
					month: 'long',
					day: 'numeric'
				})
			: 'Unknown'
	);

	let lastActive = $derived(
		user?.lastLogin
			? new Date(Number(user.lastLogin.seconds) * 1000).toLocaleString(undefined, {
					year: 'numeric',
					month: 'short',
					day: 'numeric',
					hour: '2-digit',
					minute: '2-digit'
				})
			: null
	);

	let providerLabel = $derived((user?.authProvider || 'local').toUpperCase());

	async function changePassword() {
		if (!passwordForm.oldPassword || !passwordForm.newPassword) {
			toast.error('Please fill in all fields');
			return;
		}

		if (passwordForm.newPassword !== passwordForm.confirmPassword) {
			toast.error('New passwords do not match');
			return;
		}

		if (passwordForm.newPassword.length < 8) {
			toast.error('New password must be at least 8 characters');
			return;
		}

		saving = true;
		try {
			await authStore.changePassword(passwordForm.oldPassword, passwordForm.newPassword);
			toast.success('Password changed successfully');
			passwordForm = {
				oldPassword: '',
				newPassword: '',
				confirmPassword: ''
			};
		} catch (error: any) {
			toast.error(error.message || 'Failed to change password');
		} finally {
			saving = false;
		}
	}
</script>

<div class="flex-1 space-y-8 p-8 pt-6">
	{#if user}
		<!-- Header with Avatar -->
		<div class="flex items-center gap-6 pb-6 border-b-2 border-border/50">
			<div class="h-16 w-16 rounded-2xl bg-gradient-to-br from-primary to-primary/70 flex items-center justify-center shadow-lg">
				<span class="text-2xl font-bold text-primary-foreground">{initials}</span>
			</div>
			<div class="space-y-1">
				<div class="flex items-center gap-3">
					<h2 class="text-4xl font-bold tracking-tight bg-gradient-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">{user.username}</h2>
					<Badge variant={getRoleBadgeVariant(primaryRole)} class="text-sm">{primaryRole}</Badge>
				</div>
				<p class="text-base text-muted-foreground">Manage your account settings and security</p>
			</div>
		</div>

		<div class="grid gap-6 md:grid-cols-2">
			<!-- Account Information -->
			<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center">
							<User class="h-5 w-5 text-primary" />
						</div>
						<div>
							<CardTitle>Account Information</CardTitle>
							<CardDescription>Your account details and roles</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent class="relative space-y-4">
					<!-- Username -->
					<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
						<User class="h-4 w-4 text-muted-foreground shrink-0" />
						<div>
							<p class="text-xs text-muted-foreground">Username</p>
							<p class="text-sm font-medium">{user.username}</p>
						</div>
					</div>

					<!-- Provider -->
					<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
						<Shield class="h-4 w-4 text-muted-foreground shrink-0" />
						<div class="flex items-center gap-2">
							<div>
								<p class="text-xs text-muted-foreground">Auth Provider</p>
								<p class="text-sm font-medium">{providerLabel}</p>
							</div>
						</div>
						<Badge variant="outline" class="ml-auto text-xs">{providerLabel}</Badge>
					</div>

					<!-- Email -->
					{#if user.email}
						<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
							<Mail class="h-4 w-4 text-muted-foreground shrink-0" />
							<div>
								<p class="text-xs text-muted-foreground">Email</p>
								<p class="text-sm font-medium">{user.email}</p>
							</div>
						</div>
					{/if}

					<!-- Roles -->
					<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
						<Shield class="h-4 w-4 text-muted-foreground shrink-0" />
						<div class="flex-1">
							<p class="text-xs text-muted-foreground mb-1">Roles</p>
							<div class="flex flex-wrap gap-1.5">
								{#each user.roles || [] as role}
									<Badge variant={getRoleBadgeVariant(role)}>{role}</Badge>
								{/each}
								{#if !user.roles?.length}
									<span class="text-muted-foreground text-xs">No roles assigned</span>
								{/if}
							</div>
						</div>
					</div>

					<!-- Member Since -->
					<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
						<Calendar class="h-4 w-4 text-muted-foreground shrink-0" />
						<div>
							<p class="text-xs text-muted-foreground">Member since</p>
							<p class="text-sm font-medium">{memberSince}</p>
						</div>
					</div>

					<!-- Last Active -->
					{#if lastActive}
						<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
							<Clock class="h-4 w-4 text-muted-foreground shrink-0" />
							<div>
								<p class="text-xs text-muted-foreground">Last active</p>
								<p class="text-sm font-medium">{lastActive}</p>
							</div>
						</div>
					{/if}

					<!-- Account Status -->
					<div class="flex items-center gap-3 p-3 rounded-lg border bg-card">
						<Activity class="h-4 w-4 text-muted-foreground shrink-0" />
						<div>
							<p class="text-xs text-muted-foreground">Account status</p>
							<p class="text-sm font-medium">{user.isActive ? 'Active' : 'Inactive'}</p>
						</div>
						<Badge variant={user.isActive ? 'default' : 'destructive'} class="ml-auto">
							{user.isActive ? 'Active' : 'Inactive'}
						</Badge>
					</div>
				</CardContent>
			</Card>

			<!-- Security Card -->
			<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center">
							<Key class="h-5 w-5 text-primary" />
						</div>
						<div>
							<CardTitle>Security</CardTitle>
							<CardDescription>Password and session management</CardDescription>
						</div>
					</div>
				</CardHeader>
				<CardContent class="relative space-y-6">
					<!-- Session Info -->
					<div class="space-y-3">
						<Label class="text-sm font-medium text-muted-foreground">Session</Label>
						<div class="grid gap-2">
							<div class="flex items-center justify-between p-2.5 rounded-lg border bg-card">
								<span class="text-xs text-muted-foreground">Provider</span>
								<Badge variant="outline" class="text-xs">{providerLabel}</Badge>
							</div>
						</div>
					</div>

					<!-- Password Form (local users only) -->
					{#if user.authProvider === 'local' || !user.authProvider}
						<div class="border-t pt-5">
							<Label class="text-sm font-medium text-muted-foreground mb-3 block">Change Password</Label>
							<form onsubmit={(e) => { e.preventDefault(); changePassword(); }} class="space-y-3">
								<div class="space-y-1.5">
									<Label for="old-password" class="text-xs">Current Password</Label>
									<Input
										id="old-password"
										type="password"
										bind:value={passwordForm.oldPassword}
										required
										disabled={saving}
									/>
								</div>

								<div class="space-y-1.5">
									<Label for="new-password" class="text-xs">New Password</Label>
									<Input
										id="new-password"
										type="password"
										bind:value={passwordForm.newPassword}
										required
										disabled={saving}
										placeholder="Minimum 8 characters"
									/>
								</div>

								<div class="space-y-1.5">
									<Label for="confirm-password" class="text-xs">Confirm New Password</Label>
									<Input
										id="confirm-password"
										type="password"
										bind:value={passwordForm.confirmPassword}
										required
										disabled={saving}
									/>
								</div>

								<Button type="submit" disabled={saving} class="w-full">
									{#if saving}
										<Loader2 class="mr-2 h-4 w-4 animate-spin" />
										Changing Password...
									{:else}
										<Key class="mr-2 h-4 w-4" />
										Change Password
									{/if}
								</Button>
							</form>
						</div>
					{:else}
						<div class="border-t pt-5">
							<div class="rounded-lg border border-dashed p-4 text-center">
								<Key class="h-6 w-6 text-muted-foreground mx-auto mb-2" />
								<p class="text-sm text-muted-foreground">
									Your account uses <span class="font-medium">{providerLabel}</span> authentication. Password changes are managed by your identity provider.
								</p>
							</div>
						</div>
					{/if}
				</CardContent>
			</Card>
		</div>
	{/if}
</div>
