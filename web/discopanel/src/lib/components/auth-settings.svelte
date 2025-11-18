<script lang="ts">
	import { onMount } from 'svelte';
	import { authStore, isAdmin } from '$lib/stores/auth';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Switch } from '$lib/components/ui/switch';
	import { Label } from '$lib/components/ui/label';
	import { Input } from '$lib/components/ui/input';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Shield, AlertCircle, Users, Key, Clock, UserPlus, Loader2 } from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import { UpdateAuthConfigRequestSchema, RegisterRequestSchema } from '$lib/proto/discopanel/v1/auth_pb';

	let authConfig = $state({
		enabled: false,
		sessionTimeout: 86400,
		requireEmailVerify: false,
		allowRegistration: false
	});
	
	let loading = $state(false);
	let saving = $state(false);
	let userCount = $state(0);
	let showFirstUserDialog = $state(false);
	
	// First user creation form
	let firstUserForm = $state({
		username: '',
		email: '',
		password: '',
		confirmPassword: ''
	});
	
	// Convert seconds to hours for display
	let sessionTimeoutHours = $state(24);
	
	$effect(() => {
		sessionTimeoutHours = Math.floor(authConfig.sessionTimeout / 3600);
	});
	
	async function loadAuthConfig() {
		loading = true;
		try {
			// Get auth config
			const configResponse = await rpcClient.auth.getAuthConfig({});
			authConfig = {
				enabled: configResponse.enabled,
				sessionTimeout: configResponse.sessionTimeout,
				requireEmailVerify: configResponse.requireEmailVerify,
				allowRegistration: configResponse.allowRegistration
			};
			sessionTimeoutHours = Math.floor(configResponse.sessionTimeout / 3600);

			// Get user count if we can
			try {
				const usersResponse = await rpcClient.user.listUsers({});
				userCount = usersResponse.users.length;
			} catch {
				// Ignore - probably auth is disabled or no permission
			}
		} catch (error) {
			console.error('Failed to load auth config:', error);
		} finally {
			loading = false;
		}
	}
	
	async function saveAuthConfig() {
		saving = true;
		try {
			const request = create(UpdateAuthConfigRequestSchema, {
				enabled: authConfig.enabled,
				sessionTimeout: sessionTimeoutHours * 3600,
				requireEmailVerify: authConfig.requireEmailVerify,
				allowRegistration: authConfig.allowRegistration
			});

			const result = await rpcClient.auth.updateAuthConfig(request);

			// Check if we need to create first user
			if (result.requiresFirstUser) {
				showFirstUserDialog = true;
				saving = false;
				return;
			}

			// Update auth store
			await authStore.checkAuthStatus();

			toast.success('Authentication settings saved');

			// If auth was just enabled, redirect to login
			if (authConfig.enabled && userCount > 0) {
				toast.info('Authentication enabled. Please log in.');
				setTimeout(() => {
					window.location.href = '/login';
				}, 2000);
			}
		} catch (error: any) {
			toast.error(error.message || 'Failed to save authentication settings');
			console.error(error);
		} finally {
			saving = false;
		}
	}
	
	async function createFirstUser() {
		if (firstUserForm.password !== firstUserForm.confirmPassword) {
			toast.error('Passwords do not match');
			return;
		}

		if (firstUserForm.password.length < 8) {
			toast.error('Password must be at least 8 characters');
			return;
		}

		saving = true;
		try {
			// Create the first admin user
			const request = create(RegisterRequestSchema, {
				username: firstUserForm.username,
				email: firstUserForm.email,
				password: firstUserForm.password
			});

			await rpcClient.auth.register(request);

			// Now enable authentication
			authConfig.enabled = true;
			await saveAuthConfig();

			showFirstUserDialog = false;
			toast.success('Admin account created and authentication enabled');

			// Redirect to login
			setTimeout(() => {
				window.location.href = '/login';
			}, 2000);
		} catch (error: any) {
			toast.error(error.message || 'Failed to create admin account');
		} finally {
			saving = false;
		}
	}
	
	onMount(() => {
		loadAuthConfig();
	});
</script>

<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
	<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
	<CardHeader class="relative pb-6">
		<div class="flex items-center gap-3">
			<div class="h-12 w-12 rounded-lg bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center">
				<Shield class="h-6 w-6 text-primary" />
			</div>
			<div>
				<CardTitle class="text-2xl font-semibold">Authentication Settings</CardTitle>
				<CardDescription class="text-base mt-1">
					Configure user authentication and access control
				</CardDescription>
			</div>
		</div>
	</CardHeader>
	<CardContent class="relative space-y-6">
		{#if loading}
			<div class="flex items-center justify-center py-16">
				<div class="text-center space-y-3">
					<Loader2 class="h-8 w-8 mx-auto animate-spin text-primary" />
					<div class="text-muted-foreground font-medium">Loading authentication settings...</div>
				</div>
			</div>
		{:else}
			<div class="space-y-6">
				<!-- Enable Authentication -->
				<div class="flex items-center justify-between p-4 rounded-lg border bg-card">
					<div class="space-y-0.5">
						<Label for="auth-enabled" class="text-base font-medium">Enable Authentication</Label>
						<p class="text-sm text-muted-foreground">
							Require users to log in to access DiscoPanel
						</p>
					</div>
					<Switch
						id="auth-enabled"
						checked={authConfig.enabled}
						onCheckedChange={(checked) => authConfig.enabled = checked}
						disabled={saving}
					/>
				</div>
				
				{#if authConfig.enabled}
					<!-- Session Timeout -->
					<div class="space-y-2">
						<Label for="session-timeout" class="flex items-center gap-2">
							<Clock class="h-4 w-4" />
							Session Timeout (hours)
						</Label>
						<Input
							id="session-timeout"
							type="number"
							min="1"
							max="720"
							bind:value={sessionTimeoutHours}
							disabled={saving}
							class="max-w-xs"
						/>
						<p class="text-sm text-muted-foreground">
							How long users stay logged in (default: 24 hours)
						</p>
					</div>
					
					<!-- Allow Registration -->
					<div class="flex items-center justify-between p-4 rounded-lg border bg-card">
						<div class="space-y-0.5">
							<Label for="allow-registration" class="text-base font-medium flex items-center gap-2">
								<UserPlus class="h-4 w-4" />
								Allow Registration
							</Label>
							<p class="text-sm text-muted-foreground">
								Allow new users to create accounts (they'll have viewer role by default)
							</p>
						</div>
						<Switch
							id="allow-registration"
							checked={authConfig.allowRegistration}
							onCheckedChange={(checked) => authConfig.allowRegistration = checked}
							disabled={saving}
						/>
					</div>
					
					{#if userCount > 0}
						<!-- User Statistics -->
						<div class="p-4 rounded-lg border bg-muted/50">
							<div class="flex items-center gap-2 mb-2">
								<Users class="h-4 w-4 text-muted-foreground" />
								<span class="text-sm font-medium">User Statistics</span>
							</div>
							<p class="text-sm text-muted-foreground">
								Total users: <span class="font-medium text-foreground">{userCount}</span>
							</p>
						</div>
					{/if}
					
					<!-- Recovery Key Info -->
					<Alert>
						<Key class="h-4 w-4" />
						<AlertDescription>
							A recovery key will be generated and saved to the server's data directory when authentication is enabled. 
							Keep this key secure - it can be used to reset any user's password.
						</AlertDescription>
					</Alert>
				{/if}
			</div>
			
			<div class="flex justify-end pt-4">
				<Button onclick={saveAuthConfig} disabled={saving}>
					{#if saving}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Saving...
					{:else}
						Save Settings
					{/if}
				</Button>
			</div>
		{/if}
	</CardContent>
</Card>

<!-- First User Creation Dialog -->
<Dialog open={showFirstUserDialog} onOpenChange={(open) => showFirstUserDialog = open}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create Admin Account</DialogTitle>
			<DialogDescription>
				Create the first admin account to enable authentication. This account will have full system access.
			</DialogDescription>
		</DialogHeader>
		
		<div class="space-y-4">
			<div class="space-y-2">
				<Label for="first-username">Username</Label>
				<Input
					id="first-username"
					type="text"
					bind:value={firstUserForm.username}
					placeholder="admin"
					required
				/>
			</div>
			<div class="space-y-2">
				<Label for="first-email">Email (optional)</Label>
				<Input
					id="first-email"
					type="email"
					bind:value={firstUserForm.email}
					placeholder="admin@example.com"
				/>
			</div>
			<div class="space-y-2">
				<Label for="first-password">Password</Label>
				<Input
					id="first-password"
					type="password"
					bind:value={firstUserForm.password}
					placeholder="Choose a strong password"
					required
				/>
			</div>
			<div class="space-y-2">
				<Label for="first-confirm">Confirm Password</Label>
				<Input
					id="first-confirm"
					type="password"
					bind:value={firstUserForm.confirmPassword}
					placeholder="Confirm your password"
					required
				/>
			</div>
		</div>
		
		<DialogFooter>
			<Button variant="outline" onclick={() => showFirstUserDialog = false} disabled={saving}>
				Cancel
			</Button>
			<Button onclick={createFirstUser} disabled={saving}>
				{#if saving}
					<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					Creating...
				{:else}
					Create Admin & Enable Auth
				{/if}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>