<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { authStore } from '$lib/stores/auth';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Tabs, TabsContent, TabsList, TabsTrigger } from '$lib/components/ui/tabs';
	import { toast } from 'svelte-sonner';
	import { Loader2, AlertCircle } from '@lucide/svelte';

	let mode = $state<'login' | 'register' | 'reset'>('login');
	let username = $state('');
	let email = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let recoveryKey = $state('');
	let loading = $state(false);
	let error = $state('');
	let authStatus = $state({
		enabled: false,
		firstUserSetup: false,
		allowRegistration: false
	});

	onMount(() => {
		// If already authenticated, redirect to home
		if ($authStore.isAuthenticated) {
			goto('/');
		}

		// Check auth status
		authStore.checkAuthStatus().then(status => {
			authStatus = status;
			
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
			goto('/');
		} catch (err: any) {
			error = err.message || 'Login failed';
		} finally {
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
			goto('/');
		} catch (err: any) {
			error = err.message || 'Registration failed';
		} finally {
			loading = false;
		}
	}

	async function handleReset() {
		error = '';
		loading = true;
		
		try {
			await authStore.resetPassword(username, recoveryKey, password);
			toast.success('Password reset successfully');
			mode = 'login';
			password = '';
			recoveryKey = '';
		} catch (err: any) {
			error = err.message || 'Password reset failed';
		} finally {
			loading = false;
		}
	}

	function handleSubmit(e: Event) {
		e.preventDefault();
		
		if (mode === 'login') {
			handleLogin();
		} else if (mode === 'register') {
			handleRegister();
		} else {
			handleReset();
		}
	}
</script>

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

			{#if !authStatus.firstUserSetup}
				<Tabs bind:value={mode} class="w-full">
					<TabsList class="grid w-full grid-cols-{authStatus.allowRegistration ? 3 : 2}">
						<TabsTrigger value="login">Login</TabsTrigger>
						{#if authStatus.allowRegistration}
							<TabsTrigger value="register">Register</TabsTrigger>
						{/if}
						<TabsTrigger value="reset">Reset</TabsTrigger>
					</TabsList>
					
					<TabsContent value="login">
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
					</TabsContent>
					
					{#if authStatus.allowRegistration}
						<TabsContent value="register">
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
						</TabsContent>
					{/if}
					
					<TabsContent value="reset">
						<form onsubmit={handleSubmit} class="space-y-4">
							<p class="text-sm text-muted-foreground mb-4">
								Enter your username, recovery key, and new password to reset your account.
							</p>
							<div class="space-y-2">
								<Label for="reset-username">Username</Label>
								<Input
									id="reset-username"
									type="text"
									bind:value={username}
									required
									disabled={loading}
									placeholder="Your username"
								/>
							</div>
							<div class="space-y-2">
								<Label for="recovery-key">Recovery Key</Label>
								<Input
									id="recovery-key"
									type="text"
									bind:value={recoveryKey}
									required
									disabled={loading}
									placeholder="Enter recovery key"
								/>
							</div>
							<div class="space-y-2">
								<Label for="new-password">New Password</Label>
								<Input
									id="new-password"
									type="password"
									bind:value={password}
									required
									disabled={loading}
									placeholder="Choose new password"
								/>
							</div>
							<Button type="submit" class="w-full" disabled={loading}>
								{#if loading}
									<Loader2 class="mr-2 h-4 w-4 animate-spin" />
									Resetting password...
								{:else}
									Reset Password
								{/if}
							</Button>
						</form>
					</TabsContent>
				</Tabs>
			{:else}
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
							This will be the admin account with full system access. 
							A recovery key will be generated and saved for password recovery.
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
			{/if}
		</CardContent>
	</Card>
</div>