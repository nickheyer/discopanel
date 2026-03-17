<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { create } from '@bufbuild/protobuf';
	import { authStore } from '$lib/stores/auth';
	import { rpcClient } from '$lib/api/rpc-client';
	import { silentCallOptions } from '$lib/api/rpc-client';
	import { ValidateInviteRequestSchema } from '$lib/proto/discopanel/v1/auth_pb';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import { Loader2, AlertCircle, TicketCheck, KeyRound } from '@lucide/svelte';

	let mode = $state<'login' | 'register'>('login');
	let username = $state('');
	let email = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let loading = $state(false);
	let error = $state('');
	let authStatus = $state({
		enabled: false,
		firstUserSetup: false,
		allowRegistration: false
	});
	let oidcEnabled = $state(false);
	let localAuthEnabled = $state(true);

	// Recovery state
	let showRecovery = $state(false);
	let recoveryKey = $state('');

	// Invite state
	let inviteCode = $state('');
	let inviteValid = $state(false);
	let inviteRequiresPin = $state(false);
	let inviteDescription = $state('');
	let invitePin = $state('');

	onMount(() => {
		// Check for OIDC callback token
		const urlParams = new URLSearchParams(window.location.search);
		const token = urlParams.get('token');
		if (token) {
			// Store token from OIDC callback in both localStorage and store state
			authStore.setToken(token);
			window.history.replaceState({}, '', '/login');
			authStore.validateSession().then(valid => {
				if (valid) {
					goto(resolve('/'));
				} else {
					error = 'Session validation failed. Please try again.';
				}
			});
			return;
		}

		// Check for invite code in URL
		const invite = urlParams.get('invite');

		// If already authenticated, redirect to home
		if ($authStore.isAuthenticated) {
			goto(resolve('/'));
			return;
		}

		// Check auth status
		authStore.checkAuthStatus().then(async (status) => {
			authStatus = status;
			oidcEnabled = $authStore.oidcEnabled;
			localAuthEnabled = $authStore.localAuthEnabled;

			// If auth is disabled and not first user setup, redirect to home
			if (!status.enabled && !status.firstUserSetup) {
				goto(resolve('/'));
				return;
			}

			// If first user setup, show registration
			if (status.firstUserSetup) {
				mode = 'register';
				return;
			}

			// Validate invite code if present
			if (invite) {
				try {
					const resp = await rpcClient.auth.validateInvite(
						create(ValidateInviteRequestSchema, { code: invite }),
						silentCallOptions,
					);
					if (resp.valid) {
						inviteCode = invite;
						inviteValid = true;
						inviteRequiresPin = resp.requiresPin;
						inviteDescription = resp.description;
						mode = 'register';
					}
				} catch {
					// Invalid invite, just show normal login
				}
				// Clean up URL
				window.history.replaceState({}, '', '/login');
			}
		});
	});

	async function handleLogin() {
		error = '';
		loading = true;

		try {
			await authStore.login(username, password);
			toast.success('Logged in successfully');
			setTimeout(() => {
				goto(resolve('/'));
			}, 100);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Login failed';
			loading = false;
		}
	}

	async function handleRegister() {
		error = '';

		if (password !== confirmPassword) {
			error = 'Passwords do not match';
			return;
		}

		if (password.length < 8) {
			error = 'Password must be at least 8 characters';
			return;
		}

		loading = true;

		try {
			await authStore.register(
				username,
				email,
				password,
				inviteValid ? inviteCode : undefined,
				inviteValid && inviteRequiresPin ? invitePin : undefined,
			);
			toast.success(authStatus.firstUserSetup ?
				'Admin account created successfully' :
				'Account created successfully');
			setTimeout(() => {
				goto(resolve('/'));
			}, 100);
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Registration failed';
			loading = false;
		}
	}

	async function handleOIDCLogin() {
		try {
			const response = await (await import('$lib/api/rpc-client')).rpcClient.auth.getOIDCLoginURL({});
			if (response.loginUrl) {
				window.location.href = response.loginUrl;
			}
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Failed to initiate SSO login';
		}
	}

	async function handleRecovery() {
		error = '';
		loading = true;
		try {
			await authStore.useRecoveryKey(recoveryKey);
			toast.success('Panel reset to first-user setup');
			window.location.reload();
		} catch (err: unknown) {
			error = err instanceof Error ? err.message : 'Invalid recovery key';
			loading = false;
		}
	}

	function handleSubmit(e: Event) {
		e.preventDefault();

		if (mode === 'login') {
			handleLogin();
		} else {
			handleRegister();
		}
	}
</script>

{#snippet loginForm()}
	<div class="space-y-4">
		{#if localAuthEnabled}
			<form onsubmit={handleSubmit} class="space-y-4">
				<div class="space-y-2">
					<Label for="username">Username</Label>
					<Input
						id="username"
						type="text"
						bind:value={username}
						required
						disabled={loading}
						placeholder="Enter your username"
					/>
				</div>
				<div class="space-y-2">
					<Label for="password">Password</Label>
					<Input
						id="password"
						type="password"
						bind:value={password}
						required
						disabled={loading}
						placeholder="Enter your password"
					/>
				</div>
				<Button type="submit" class="w-full" disabled={loading}>
					{#if loading}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
						Signing in...
					{:else}
						Sign In
					{/if}
				</Button>
			</form>
		{/if}

		{#if oidcEnabled}
			{#if localAuthEnabled}
				<div class="relative my-4">
					<div class="absolute inset-0 flex items-center">
						<span class="w-full border-t"></span>
					</div>
					<div class="relative flex justify-center text-xs uppercase">
						<span class="bg-background px-2 text-muted-foreground">Or</span>
					</div>
				</div>
			{/if}
			<Button type="button" variant={localAuthEnabled ? 'outline' : 'default'} class="w-full" onclick={handleOIDCLogin} disabled={loading}>
				Sign in with SSO
			</Button>
		{/if}
	</div>
{/snippet}

{#snippet registerForm()}
	<form onsubmit={handleSubmit} class="space-y-4">
		{#if inviteValid && inviteDescription}
			<Alert>
				<TicketCheck class="h-4 w-4" />
				<AlertDescription>{inviteDescription}</AlertDescription>
			</Alert>
		{/if}
		<div class="space-y-2">
			<Label for="reg-username">Username</Label>
			<Input
				id="reg-username"
				type="text"
				bind:value={username}
				required
				disabled={loading}
				placeholder="Choose a username"
			/>
		</div>
		<div class="space-y-2">
			<Label for="reg-email">Email (optional)</Label>
			<Input
				id="reg-email"
				type="email"
				bind:value={email}
				disabled={loading}
				placeholder="your@email.com"
			/>
		</div>
		<div class="space-y-2">
			<Label for="reg-password">Password</Label>
			<Input
				id="reg-password"
				type="password"
				bind:value={password}
				required
				disabled={loading}
				placeholder="Choose a password"
			/>
		</div>
		<div class="space-y-2">
			<Label for="reg-confirm">Confirm Password</Label>
			<Input
				id="reg-confirm"
				type="password"
				bind:value={confirmPassword}
				required
				disabled={loading}
				placeholder="Confirm your password"
			/>
		</div>
		{#if inviteValid && inviteRequiresPin}
			<div class="space-y-2">
				<Label for="reg-pin">Invite PIN</Label>
				<Input
					id="reg-pin"
					type="password"
					bind:value={invitePin}
					required
					disabled={loading}
					placeholder="Enter invite PIN"
				/>
			</div>
		{/if}
		<Button type="submit" class="w-full" disabled={loading}>
			{#if loading}
				<Loader2 class="mr-2 h-4 w-4 animate-spin" />
				Creating account...
			{:else}
				Create Account
			{/if}
		</Button>
	</form>
{/snippet}

<div class="min-h-screen flex items-center justify-center bg-background p-4">
	<Card class="w-full max-w-md">
		<CardHeader class="space-y-1">
			<div class="flex items-center justify-center mb-4">
				<img src="/g1_24x24.png" alt="DiscoPanel Logo" class="h-8 w-8 mr-2" />
				<CardTitle class="text-2xl">DiscoPanel</CardTitle>
			</div>
			{#if authStatus.firstUserSetup}
				<CardDescription class="text-center">
					Welcome! Create your admin account to get started.
				</CardDescription>
			{:else}
				<CardDescription class="text-center">
					Sign in to manage your Minecraft servers
				</CardDescription>
			{/if}
		</CardHeader>

		<CardContent>
			{#if error}
				<Alert variant="destructive" class="mb-4">
					<AlertCircle class="h-4 w-4" />
					<AlertDescription>{error}</AlertDescription>
				</Alert>
			{/if}

			{#if authStatus.firstUserSetup}
				<!-- First user setup -->
				<form onsubmit={handleSubmit} class="space-y-4">
					<div class="space-y-2">
						<Label for="admin-username">Admin Username</Label>
						<Input
							id="admin-username"
							type="text"
							bind:value={username}
							required
							disabled={loading}
							placeholder="Choose admin username"
						/>
					</div>
					<div class="space-y-2">
						<Label for="admin-email">Email (optional)</Label>
						<Input
							id="admin-email"
							type="email"
							bind:value={email}
							disabled={loading}
							placeholder="admin@example.com"
						/>
					</div>
					<div class="space-y-2">
						<Label for="admin-password">Password</Label>
						<Input
							id="admin-password"
							type="password"
							bind:value={password}
							required
							disabled={loading}
							placeholder="Choose a strong password"
						/>
					</div>
					<div class="space-y-2">
						<Label for="admin-confirm">Confirm Password</Label>
						<Input
							id="admin-confirm"
							type="password"
							bind:value={confirmPassword}
							required
							disabled={loading}
							placeholder="Confirm your password"
						/>
					</div>
					<Alert>
						<AlertCircle class="h-4 w-4" />
						<AlertDescription>
							{#if oidcEnabled}
								A local admin account is required for initial setup, even with SSO enabled. This ensures you always have a fallback login to manage the system if your identity provider becomes unavailable.
							{:else}
								This will be the admin account with full system access.
							{/if}
						</AlertDescription>
					</Alert>
					<Button type="submit" class="w-full" disabled={loading}>
						{#if loading}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Creating admin account...
						{:else}
							Create Admin Account
						{/if}
					</Button>
				</form>
			{:else if (authStatus.allowRegistration || inviteValid) && localAuthEnabled}
				<!-- Login + Registration tabs -->
				<Tabs bind:value={mode} class="w-full">
					<TabsList class="grid w-full grid-cols-2">
						<TabsTrigger value="login">Login</TabsTrigger>
						<TabsTrigger value="register">Register</TabsTrigger>
					</TabsList>

					<TabsContent value="login">
						{@render loginForm()}
					</TabsContent>

					<TabsContent value="register">
						{@render registerForm()}
					</TabsContent>
				</Tabs>
			{:else}
				<!-- Login only (no registration) / SSO only -->
				{@render loginForm()}
			{/if}

			{#if showRecovery}
				<div class="space-y-4 mt-4">
					<Alert variant="destructive">
						<AlertCircle class="h-4 w-4" />
						<AlertDescription>
							This will delete all users, sessions, and invites. Server configs and data are preserved. This cannot be undone.
						</AlertDescription>
					</Alert>
					<div class="space-y-2">
						<Label for="recovery-key">Recovery Key</Label>
						<Input
							id="recovery-key"
							type="password"
							bind:value={recoveryKey}
							disabled={loading}
							placeholder="Paste your recovery key"
						/>
					</div>
					<div class="flex gap-2">
						<Button variant="outline" class="flex-1" onclick={() => { showRecovery = false; error = ''; }} disabled={loading}>
							Cancel
						</Button>
						<Button variant="destructive" class="flex-1" onclick={handleRecovery} disabled={loading || !recoveryKey}>
							{#if loading}
								<Loader2 class="mr-2 h-4 w-4 animate-spin" />
								Resetting...
							{:else}
								Reset Panel
							{/if}
						</Button>
					</div>
				</div>
			{:else if !authStatus.firstUserSetup}
				<div class="mt-4 text-center">
					<button
						type="button"
						class="text-xs text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1"
						onclick={() => showRecovery = true}
					>
						<KeyRound class="h-3 w-3" />
						Forgot access? Recovery
					</button>
				</div>
			{/if}

			{#if $authStore.anonymousAccessEnabled}
				<div class="relative my-4">
					<div class="absolute inset-0 flex items-center">
						<span class="w-full border-t"></span>
					</div>
					<div class="relative flex justify-center text-xs uppercase">
						<span class="bg-background px-2 text-muted-foreground">Or</span>
					</div>
				</div>
				<Button variant="ghost" class="w-full" onclick={() => goto(resolve('/'))}>
					Continue as Guest
				</Button>
			{/if}
		</CardContent>
	</Card>
</div>
