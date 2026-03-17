<script lang="ts">
	import { authStore, currentUser } from '$lib/stores/auth';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Dialog, DialogContent } from '$lib/components/ui/dialog';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { toast } from 'svelte-sonner';
	import { User, Key, Loader2, Mail, Calendar, Clock, Shield, Activity, Plus, Trash2, Copy, X, Check, AlertTriangle, KeyRound } from '@lucide/svelte';
	import { getRoleBadgeVariant } from '$lib/utils/role-colors';
	import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';
	import { onMount } from 'svelte';
	import type { ApiToken } from '$lib/proto/discopanel/v1/auth_pb';

	let user = $derived($currentUser);
	let passwordForm = $state({
		oldPassword: '',
		newPassword: '',
		confirmPassword: ''
	});
	let saving = $state(false);

	// API Tokens state
	let apiTokens = $state<ApiToken[]>([]);
	let loadingTokens = $state(false);
	let showCreateTokenDialog = $state(false);
	let creatingToken = $state(false);
	let newTokenForm = $state({ name: '', expiresInDays: '' as string });
	let createdToken = $state<string | null>(null);
	let copied = $state(false);
	let deletingTokenId = $state<string | null>(null);

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

	onMount(() => {
		loadTokens();
	});

	async function loadTokens() {
		loadingTokens = true;
		try {
			const resp = await rpcClient.auth.listAPITokens({}, silentCallOptions);
			apiTokens = resp.apiTokens;
		} catch {
			// silently fail - tokens will show empty
		} finally {
			loadingTokens = false;
		}
	}

	async function createToken() {
		if (!newTokenForm.name.trim()) {
			toast.error('Token name is required');
			return;
		}

		creatingToken = true;
		try {
			const days = newTokenForm.expiresInDays ? parseInt(newTokenForm.expiresInDays) : undefined;
			const resp = await rpcClient.auth.createAPIToken({
				name: newTokenForm.name.trim(),
				expiresInDays: days
			});
			createdToken = resp.plaintextToken;
			toast.success('API token created');
			await loadTokens();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create API token');
		} finally {
			creatingToken = false;
		}
	}

	async function deleteToken(id: string) {
		deletingTokenId = id;
		try {
			await rpcClient.auth.deleteAPIToken({ id });
			toast.success('API token deleted');
			await loadTokens();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete API token');
		} finally {
			deletingTokenId = null;
		}
	}

	async function copyToken() {
		if (!createdToken) return;
		try {
			await navigator.clipboard.writeText(createdToken);
			copied = true;
			toast.success('Token copied to clipboard');
			setTimeout(() => { copied = false; }, 2000);
		} catch {
			toast.error('Failed to copy token');
		}
	}

	function closeCreateDialog() {
		showCreateTokenDialog = false;
		createdToken = null;
		copied = false;
		newTokenForm = { name: '', expiresInDays: '' };
	}

	function formatTimestamp(ts: { seconds: bigint } | undefined): string {
		if (!ts) return 'Never';
		return new Date(Number(ts.seconds) * 1000).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}

	function isExpired(ts: { seconds: bigint } | undefined): boolean {
		if (!ts) return false;
		return new Date(Number(ts.seconds) * 1000) < new Date();
	}

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
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to change password');
		} finally {
			saving = false;
		}
	}
</script>

<div class="flex-1 space-y-8 p-8 pt-6">
	{#if user}
		<!-- Header with Avatar -->
		<div class="flex items-center gap-6 pb-6 border-b-2 border-border/50">
			<div class="h-16 w-16 rounded-2xl bg-linear-to-br from-primary to-primary/70 flex items-center justify-center shadow-lg">
				<span class="text-2xl font-bold text-primary-foreground">{initials}</span>
			</div>
			<div class="space-y-1">
				<div class="flex items-center gap-3">
					<h2 class="text-4xl font-bold tracking-tight bg-linear-to-r from-foreground to-foreground/70 bg-clip-text text-transparent">{user.username}</h2>
					<Badge variant={getRoleBadgeVariant(primaryRole)} class="text-sm">{primaryRole}</Badge>
				</div>
				<p class="text-base text-muted-foreground">Manage your account settings and security</p>
			</div>
		</div>

		<div class="grid gap-6 md:grid-cols-2">
			<!-- Account Information -->
			<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center">
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
								{#each user.roles || [] as role (role)}
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
			<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
				<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
				<CardHeader class="relative">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center">
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

		<!-- API Tokens Card (full width) -->
		<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="relative">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3">
						<div class="h-10 w-10 rounded-lg bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center">
							<KeyRound class="h-5 w-5 text-primary" />
						</div>
						<div>
							<CardTitle>API Tokens</CardTitle>
							<CardDescription>Programmatic access tokens that inherit your identity and permissions</CardDescription>
						</div>
					</div>
					<Button onclick={() => showCreateTokenDialog = true} size="sm" class="gap-1.5">
						<Plus class="h-4 w-4" />
						Create Token
					</Button>
				</div>
			</CardHeader>
			<CardContent class="relative">
				{#if loadingTokens}
					<div class="flex items-center justify-center py-8">
						<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
					</div>
				{:else if apiTokens.length === 0}
					<div class="rounded-lg border border-dashed p-8 text-center">
						<KeyRound class="h-8 w-8 text-muted-foreground mx-auto mb-3" />
						<p class="text-sm font-medium text-muted-foreground">No API tokens</p>
						<p class="text-xs text-muted-foreground mt-1">Create a token to authenticate programmatically with the DiscoPanel API.</p>
					</div>
				{:else}
					<div class="rounded-lg border overflow-hidden">
						<Table>
							<TableHeader>
								<TableRow>
									<TableHead>Name</TableHead>
									<TableHead>Created</TableHead>
									<TableHead>Expires</TableHead>
									<TableHead>Last Used</TableHead>
									<TableHead class="w-[80px]"></TableHead>
								</TableRow>
							</TableHeader>
							<TableBody>
								{#each apiTokens as token (token.id)}
									<TableRow>
										<TableCell class="font-medium">
											<div class="flex items-center gap-2">
												<KeyRound class="h-3.5 w-3.5 text-muted-foreground" />
												{token.name}
											</div>
										</TableCell>
										<TableCell class="text-muted-foreground text-sm">
											{formatTimestamp(token.createdAt)}
										</TableCell>
										<TableCell>
											{#if token.expiresAt}
												<Badge variant={isExpired(token.expiresAt) ? 'destructive' : 'outline'} class="text-xs">
													{isExpired(token.expiresAt) ? 'Expired' : formatTimestamp(token.expiresAt)}
												</Badge>
											{:else}
												<span class="text-muted-foreground text-sm">Never</span>
											{/if}
										</TableCell>
										<TableCell class="text-muted-foreground text-sm">
											{formatTimestamp(token.lastUsedAt)}
										</TableCell>
										<TableCell>
											<Button
												variant="ghost"
												size="icon"
												class="h-8 w-8 text-destructive hover:text-destructive"
												onclick={() => deleteToken(token.id)}
												disabled={deletingTokenId === token.id}
											>
												{#if deletingTokenId === token.id}
													<Loader2 class="h-4 w-4 animate-spin" />
												{:else}
													<Trash2 class="h-4 w-4" />
												{/if}
											</Button>
										</TableCell>
									</TableRow>
								{/each}
							</TableBody>
						</Table>
					</div>
				{/if}
			</CardContent>
		</Card>
	{/if}
</div>

<!-- Create API Token Dialog -->
<Dialog open={showCreateTokenDialog} onOpenChange={(open) => { if (!open) closeCreateDialog(); }}>
	<DialogContent class="!max-w-3xl !w-[90vw] !h-[70vh] !p-0 !gap-0 overflow-hidden flex flex-col" showCloseButton={false}>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="w-64 border-r bg-muted/30 flex flex-col">
				<!-- Sidebar Header -->
				<div class="p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
							<KeyRound class="h-6 w-6 text-primary" />
						</div>
						<div class="flex-1 min-w-0">
							<h3 class="font-semibold">New API Token</h3>
							<p class="text-xs text-muted-foreground mt-0.5">Programmatic access</p>
						</div>
					</div>
				</div>

				<!-- Info -->
				<div class="flex-1 p-4 space-y-4">
					<div class="space-y-3">
						<div class="flex items-start gap-3 text-sm">
							<User class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Tokens inherit your full identity, roles, and permissions.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<Shield class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Use tokens to authenticate API requests programmatically.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<AlertTriangle class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">The token value is shown only once after creation.</p>
						</div>
					</div>
				</div>

				<!-- Sidebar Footer -->
				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-1">Usage</p>
						<p class="text-xs text-muted-foreground font-mono">
							Authorization: Bearer dp_...
						</p>
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex-1 flex flex-col min-w-0">
				<!-- Content Header -->
				<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">
							{createdToken ? 'Token Created' : 'Create API Token'}
						</h2>
						<p class="text-muted-foreground mt-1">
							{createdToken ? 'Copy your token now — it won\'t be shown again' : 'Configure your new API token'}
						</p>
					</div>
					<Button variant="ghost" size="icon" onclick={closeCreateDialog} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<!-- Scrollable Content Area -->
				<div class="flex-1 overflow-y-auto p-8">
					{#if createdToken}
						<!-- Token Created View -->
						<div class="space-y-6">
							<div class="rounded-lg border-2 border-primary/50 bg-primary/5 p-6 space-y-4">
								<div class="flex items-center gap-2 text-primary">
									<Check class="h-5 w-5" />
									<span class="font-semibold">Token created successfully</span>
								</div>
								<div class="relative">
									<div class="font-mono text-sm bg-card border rounded-lg p-4 pr-12 break-all select-all">
										{createdToken}
									</div>
									<Button
										variant="ghost"
										size="icon"
										class="absolute right-2 top-2 h-8 w-8"
										onclick={copyToken}
									>
										{#if copied}
											<Check class="h-4 w-4 text-green-500" />
										{:else}
											<Copy class="h-4 w-4" />
										{/if}
									</Button>
								</div>
							</div>

							<div class="flex items-start gap-3 p-4 rounded-lg border border-destructive/30 bg-destructive/5">
								<AlertTriangle class="h-5 w-5 text-destructive shrink-0 mt-0.5" />
								<div>
									<p class="text-sm font-medium text-destructive">This token will not be shown again</p>
									<p class="text-xs text-muted-foreground mt-1">
										Make sure you copy it now. If you lose it, you'll need to create a new one.
									</p>
								</div>
							</div>

							<div class="space-y-2">
								<p class="text-sm font-medium">Example usage</p>
								<pre class="font-mono text-xs bg-card border rounded-lg p-4 overflow-x-auto whitespace-pre text-muted-foreground">curl {window.location.origin}/discopanel.v1.UserService/ListUsers \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer {createdToken}' \
  -d '{"{}"}'</pre>
							</div>
						</div>
					{:else}
						<!-- Create Form -->
						<div class="space-y-6">
							<div class="space-y-2">
								<Label for="token-name">Token Name</Label>
								<Input
									id="token-name"
									bind:value={newTokenForm.name}
									placeholder="e.g. CI/CD Pipeline, Monitoring Script"
									disabled={creatingToken}
								/>
								<p class="text-xs text-muted-foreground">A descriptive name to help you identify this token.</p>
							</div>

							<div class="space-y-2">
								<Label>Expiration</Label>
								<Select
									value={newTokenForm.expiresInDays || 'never'}
									type="single"
									onValueChange={(v) => { if (v) newTokenForm.expiresInDays = v === 'never' ? '' : v; }}
									disabled={creatingToken}
								>
									<SelectTrigger class="h-9">
										<span>
											{#if !newTokenForm.expiresInDays}
												No expiration
											{:else if newTokenForm.expiresInDays === '7'}
												7 days
											{:else if newTokenForm.expiresInDays === '30'}
												30 days
											{:else if newTokenForm.expiresInDays === '90'}
												90 days
											{:else if newTokenForm.expiresInDays === '365'}
												1 year
											{:else}
												{newTokenForm.expiresInDays} days
											{/if}
										</span>
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="never">No expiration</SelectItem>
										<SelectItem value="7">7 days</SelectItem>
										<SelectItem value="30">30 days</SelectItem>
										<SelectItem value="90">90 days</SelectItem>
										<SelectItem value="365">1 year</SelectItem>
									</SelectContent>
								</Select>
								<p class="text-xs text-muted-foreground">
									{newTokenForm.expiresInDays ? `Token will expire after ${newTokenForm.expiresInDays} days.` : 'Token will never expire. You can revoke it at any time.'}
								</p>
							</div>
						</div>
					{/if}
				</div>

				<!-- Footer -->
				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					{#if createdToken}
						<Button onclick={copyToken} variant="outline" class="h-11 px-6 gap-2">
							{#if copied}
								<Check class="h-4 w-4" />
								Copied
							{:else}
								<Copy class="h-4 w-4" />
								Copy Token
							{/if}
						</Button>
						<Button onclick={closeCreateDialog} class="h-11 px-8">
							Done
						</Button>
					{:else}
						<Button variant="outline" onclick={closeCreateDialog} disabled={creatingToken} class="h-11 px-6">
							Cancel
						</Button>
						<Button onclick={createToken} disabled={creatingToken || !newTokenForm.name.trim()} class="h-11 px-8 gap-2">
							{#if creatingToken}
								<Loader2 class="h-4 w-4 animate-spin" />
								Creating...
							{:else}
								<KeyRound class="h-4 w-4" />
								Create Token
							{/if}
						</Button>
					{/if}
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>
