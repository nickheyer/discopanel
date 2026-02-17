<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { authStore } from '$lib/stores/auth';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import { Loader2, AlertCircle } from '@lucide/svelte';

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
					goto('/');
				} else {
					error = 'Session validation failed. Please try again.';
				}
			});
			return;
		}

		// If already authenticated, redirect to home
		if ($authStore.isAuthenticated) {
			goto('/');
			return;
		}

		// Check auth status
		authStore.checkAuthStatus().then(status => {
			authStatus = status;
			oidcEnabled = $authStore.oidcEnabled;
			localAuthEnabled = $authStore.localAuthEnabled;

			// If auth is disabled and not first user setup, redirect to home
			if (!status.enabled && !status.firstUserSetup) {
				goto('/');
				return;
			}

			// If first user setup, show registration
			if (status.firstUserSetup) {
				mode = 'register';
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
				goto('/');
			}, 100);
		} catch (err: any) {
			error = err.message || 'Login failed';
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
			await authStore.register(username, email, password);
			toast.success(authStatus.firstUserSetup ?
				'Admin account created successfully' :
				'Account created successfully');
			setTimeout(() => {
				goto('/');
			}, 100);
		} catch (err: any) {
			error = err.message || 'Registration failed';
			loading = false;
		}
	}

	async function handleOIDCLogin() {
		try {
			const response = await (await import('$lib/api/rpc-client')).rpcClient.auth.getOIDCLoginURL({});
			if (response.loginUrl) {
				window.location.href = response.loginUrl;
			}
		} catch (err: any) {
			error = err.message || 'Failed to initiate SSO login';
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
			{:else if authStatus.allowRegistration && localAuthEnabled}
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

			{#if $authStore.anonymousAccessEnabled}
				<div class="relative my-4">
					<div class="absolute inset-0 flex items-center">
						<span class="w-full border-t"></span>
					</div>
					<div class="relative flex justify-center text-xs uppercase">
						<span class="bg-background px-2 text-muted-foreground">Or</span>
					</div>
				</div>
				<Button variant="ghost" class="w-full" onclick={() => goto('/')}>
					Continue as Guest
				</Button>
			{/if}
		</CardContent>
	</Card>
</div>
