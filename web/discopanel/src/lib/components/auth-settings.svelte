<script lang="ts">
	import { onMount } from 'svelte';
	import { create } from '@bufbuild/protobuf';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '$lib/components/ui/accordion';
	import { rpcClient } from '$lib/api/rpc-client';
	import { canUpdateSettings } from '$lib/stores/auth';
	import { toast } from 'svelte-sonner';
	import { UpdateAuthSettingsRequestSchema } from '$lib/proto/discopanel/v1/auth_pb';
	import type { GetAuthConfigResponse } from '$lib/proto/discopanel/v1/auth_pb';
	import {
		Shield,
		Loader2,
		Check,
		X,
		Save,
		KeyRound,
		Globe,
		FileCode,
		Container,
		Terminal,
	} from '@lucide/svelte';

	let loading = $state(true);
	let saving = $state(false);
	let config = $state<GetAuthConfigResponse | null>(null);

	// Editable form state
	let localAuthEnabled = $state(true);
	let allowRegistration = $state(false);
	let anonymousAccess = $state(false);
	let sessionTimeoutHours = $state(24);

	let canEdit = $derived($canUpdateSettings);

	let hasChanges = $derived(
		config != null && (
			localAuthEnabled !== config.localAuthEnabled ||
			allowRegistration !== config.allowRegistration ||
			anonymousAccess !== config.anonymousAccess ||
			Math.round(sessionTimeoutHours * 3600) !== config.sessionTimeout
		)
	);

	async function loadConfig() {
		loading = true;
		try {
			const response = await rpcClient.auth.getAuthConfig({});
			config = response;
			localAuthEnabled = response.localAuthEnabled;
			allowRegistration = response.allowRegistration;
			anonymousAccess = response.anonymousAccess;
			sessionTimeoutHours = Math.round((response.sessionTimeout / 3600) * 100) / 100;
		} catch (error) {
			console.error('Failed to load auth config:', error);
		} finally {
			loading = false;
		}
	}

	async function saveSettings() {
		if (!config) return;
		saving = true;
		try {
			const req = create(UpdateAuthSettingsRequestSchema, {});

			if (localAuthEnabled !== config.localAuthEnabled) {
				req.localAuthEnabled = localAuthEnabled;
			}
			if (allowRegistration !== config.allowRegistration) {
				req.allowRegistration = allowRegistration;
			}
			if (anonymousAccess !== config.anonymousAccess) {
				req.anonymousAccess = anonymousAccess;
			}
			const newTimeout = Math.round(sessionTimeoutHours * 3600);
			if (newTimeout !== config.sessionTimeout) {
				req.sessionTimeout = newTimeout;
			}

			const response = await rpcClient.auth.updateAuthSettings(req);
			if (response.config) {
				config = response.config;
				localAuthEnabled = response.config.localAuthEnabled;
				allowRegistration = response.config.allowRegistration;
				anonymousAccess = response.config.anonymousAccess;
				sessionTimeoutHours = Math.round((response.config.sessionTimeout / 3600) * 100) / 100;
			}
			toast.success('Authentication settings updated');
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to update settings');
		} finally {
			saving = false;
		}
	}

	onMount(() => {
		loadConfig();
	});
</script>

{#if loading}
	<div class="flex items-center justify-center py-16">
		<Loader2 class="h-8 w-8 animate-spin text-primary" />
	</div>
{:else if config}
	<div class="grid gap-6 lg:grid-cols-2">
		<!-- Card 1: Authentication Settings -->
		<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="relative pb-4">
				<div class="flex items-center gap-3">
					<div class="h-12 w-12 rounded-lg bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center">
						<Shield class="h-6 w-6 text-primary" />
					</div>
					<div>
						<CardTitle class="text-2xl font-semibold">Authentication Settings</CardTitle>
						<CardDescription class="text-base mt-1">
							Manage login methods and access controls
						</CardDescription>
					</div>
				</div>
			</CardHeader>
			<CardContent class="relative space-y-5">
				<!-- Local Authentication -->
				<div class="flex items-center justify-between p-3 rounded-lg border bg-card">
					<div class="space-y-0.5">
						<Label class="text-sm font-medium">Local Authentication</Label>
						<p class="text-xs text-muted-foreground">Username and password login</p>
					</div>
					{#if canEdit}
						<Switch
							checked={localAuthEnabled}
							onCheckedChange={(v) => { localAuthEnabled = v; if (!v) allowRegistration = false; }}
							disabled={saving}
						/>
					{:else}
						<Badge variant={localAuthEnabled ? 'default' : 'outline'}>
							{#if localAuthEnabled}
								<Check class="mr-1 h-3 w-3" /> Enabled
							{:else}
								<X class="mr-1 h-3 w-3" /> Disabled
							{/if}
						</Badge>
					{/if}
				</div>

				<!-- User Registration -->
				<div class="flex items-center justify-between p-3 rounded-lg border bg-card {!localAuthEnabled ? 'opacity-50' : ''}">
					<div class="space-y-0.5">
						<Label class="text-sm font-medium">User Registration</Label>
						<p class="text-xs text-muted-foreground">Allow new users to self-register</p>
					</div>
					{#if canEdit}
						<Switch
							checked={allowRegistration}
							onCheckedChange={(v) => { allowRegistration = v; }}
							disabled={saving || !localAuthEnabled}
						/>
					{:else}
						<Badge variant={allowRegistration ? 'default' : 'outline'}>
							{#if allowRegistration}
								<Check class="mr-1 h-3 w-3" /> Allowed
							{:else}
								<X class="mr-1 h-3 w-3" /> Disabled
							{/if}
						</Badge>
					{/if}
				</div>

				<!-- Anonymous Access -->
				<div class="flex items-center justify-between p-3 rounded-lg border bg-card">
					<div class="space-y-0.5">
						<Label class="text-sm font-medium">Anonymous Access</Label>
						<p class="text-xs text-muted-foreground">Limited unauthenticated browsing</p>
					</div>
					{#if canEdit}
						<Switch
							checked={anonymousAccess}
							onCheckedChange={(v) => { anonymousAccess = v; }}
							disabled={saving}
						/>
					{:else}
						<Badge variant={anonymousAccess ? 'default' : 'outline'}>
							{#if anonymousAccess}
								<Check class="mr-1 h-3 w-3" /> Enabled
							{:else}
								<X class="mr-1 h-3 w-3" /> Disabled
							{/if}
						</Badge>
					{/if}
				</div>

				<!-- Session Timeout -->
				<div class="flex items-center justify-between p-3 rounded-lg border bg-card">
					<div class="space-y-0.5">
						<Label class="text-sm font-medium">Session Timeout</Label>
						<p class="text-xs text-muted-foreground">How long sessions remain valid</p>
					</div>
					{#if canEdit}
						<div class="flex items-center gap-2">
							<Input
								type="number"
								min="0.084"
								step="0.5"
								class="w-20 h-8 text-sm"
								bind:value={sessionTimeoutHours}
								disabled={saving}
							/>
							<span class="text-sm text-muted-foreground">hours</span>
						</div>
					{:else}
						<Badge variant="outline">{sessionTimeoutHours}h</Badge>
					{/if}
				</div>

				{#if canEdit}
					<Button
						onclick={saveSettings}
						disabled={saving || !hasChanges}
						class="w-full"
					>
						{#if saving}
							<Loader2 class="mr-2 h-4 w-4 animate-spin" />
							Saving...
						{:else}
							<Save class="mr-2 h-4 w-4" />
							Save Changes
						{/if}
					</Button>
				{/if}
			</CardContent>
		</Card>

		<!-- Card 2: OIDC / Single Sign-On -->
		<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-linear-to-br from-card to-card/80">
			<div class="absolute inset-0 bg-linear-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
			<CardHeader class="relative pb-4">
				<div class="flex items-center gap-3">
					<div class="h-12 w-12 rounded-lg bg-linear-to-br from-primary/20 to-primary/10 flex items-center justify-center">
						<KeyRound class="h-6 w-6 text-primary" />
					</div>
					<div class="flex items-center gap-3">
						<div>
							<CardTitle class="text-2xl font-semibold">OIDC / Single Sign-On</CardTitle>
							<CardDescription class="text-base mt-1">
								External identity provider integration
							</CardDescription>
						</div>
						{#if config.oidcEnabled}
							<Badge variant="default" class="ml-2">
								<Check class="mr-1 h-3 w-3" /> Connected
							</Badge>
						{/if}
					</div>
				</div>
			</CardHeader>
			<CardContent class="relative space-y-4">
				{#if config.oidcEnabled}
					<!-- OIDC Info Grid -->
					<div class="grid gap-3">
						{#if config.oidcIssuerUri}
							<div class="p-3 rounded-lg border bg-card">
								<Label class="text-xs text-muted-foreground">Issuer URI</Label>
								<p class="text-sm font-mono mt-0.5 break-all">{config.oidcIssuerUri}</p>
							</div>
						{/if}

						{#if config.oidcClientId}
							<div class="p-3 rounded-lg border bg-card">
								<Label class="text-xs text-muted-foreground">Client ID</Label>
								<p class="text-sm font-mono mt-0.5 break-all">{config.oidcClientId}</p>
							</div>
						{/if}

						{#if config.oidcRedirectUrl}
							<div class="p-3 rounded-lg border bg-card">
								<Label class="text-xs text-muted-foreground">Redirect URL</Label>
								<p class="text-sm font-mono mt-0.5 break-all">{config.oidcRedirectUrl}</p>
							</div>
						{/if}

						{#if config.oidcScopes && config.oidcScopes.length > 0}
							<div class="p-3 rounded-lg border bg-card">
								<Label class="text-xs text-muted-foreground">Scopes</Label>
								<div class="flex flex-wrap gap-1.5 mt-1">
									{#each config.oidcScopes as scope (scope)}
										<Badge variant="secondary" class="text-xs">{scope}</Badge>
									{/each}
								</div>
							</div>
						{/if}

						{#if config.oidcRoleClaim}
							<div class="p-3 rounded-lg border bg-card">
								<Label class="text-xs text-muted-foreground">Role Claim</Label>
								<p class="text-sm font-mono mt-0.5">{config.oidcRoleClaim}</p>
							</div>
						{/if}
					</div>
				{:else}
					<!-- OIDC Not Configured -->
					<div class="rounded-lg border border-dashed p-4 text-center">
						<Globe class="h-8 w-8 text-muted-foreground mx-auto mb-2" />
						<p class="text-sm text-muted-foreground mb-1">
							OIDC allows users to sign in with external identity providers like Keycloak, Authelia, Google, or any OpenID Connect compatible service.
						</p>
						<p class="text-xs text-muted-foreground">
							OIDC must be configured outside the UI using one of the methods below.
						</p>
					</div>

					<Accordion type="single" class="w-full">
						<AccordionItem value="config-yaml">
							<AccordionTrigger class="text-sm">
								<span class="flex items-center gap-2">
									<FileCode class="h-4 w-4" />
									config.yaml
								</span>
							</AccordionTrigger>
							<AccordionContent>
								<pre class="bg-muted rounded-md p-3 text-xs overflow-x-auto"><code>auth:
  oidc:
    enabled: true
    issuer_uri: "https://your-provider/.well-known/openid-configuration"
    client_id: "discopanel"
    client_secret: "your-secret"
    redirect_url: "https://your-domain/api/v1/auth/oidc/callback"
    scopes: ["openid", "profile", "email", "groups"]
    role_claim: "groups"</code></pre>
							</AccordionContent>
						</AccordionItem>

						<AccordionItem value="docker-compose">
							<AccordionTrigger class="text-sm">
								<span class="flex items-center gap-2">
									<Container class="h-4 w-4" />
									Docker Compose
								</span>
							</AccordionTrigger>
							<AccordionContent>
								<pre class="bg-muted rounded-md p-3 text-xs overflow-x-auto"><code>services:
  discopanel:
    environment:
      DISCOPANEL_AUTH_OIDC_ENABLED: "true"
      DISCOPANEL_AUTH_OIDC_ISSUER_URI: "https://..."
      DISCOPANEL_AUTH_OIDC_CLIENT_ID: "discopanel"
      DISCOPANEL_AUTH_OIDC_CLIENT_SECRET: "your-secret"
      DISCOPANEL_AUTH_OIDC_REDIRECT_URL: "https://..."
      DISCOPANEL_AUTH_OIDC_SCOPES: "openid,profile,email,groups"
      DISCOPANEL_AUTH_OIDC_ROLE_CLAIM: "groups"</code></pre>
							</AccordionContent>
						</AccordionItem>

						<AccordionItem value="env-vars">
							<AccordionTrigger class="text-sm">
								<span class="flex items-center gap-2">
									<Terminal class="h-4 w-4" />
									Environment Variables
								</span>
							</AccordionTrigger>
							<AccordionContent>
								<div class="space-y-1.5 text-xs font-mono">
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_ENABLED</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_ISSUER_URI</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_CLIENT_ID</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_CLIENT_SECRET</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_REDIRECT_URL</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_SCOPES</code></p>
									<p><code class="bg-muted px-1.5 py-0.5 rounded">DISCOPANEL_AUTH_OIDC_ROLE_CLAIM</code></p>
								</div>
							</AccordionContent>
						</AccordionItem>
					</Accordion>

					<p class="text-xs text-muted-foreground">
						Provider examples are available in the <a href="https://docs.discopanel.app/introduction/" target="_blank" rel="noopener noreferrer" class="underline hover:text-foreground">discopanel docs</a>.
					</p>
				{/if}
			</CardContent>
		</Card>
	</div>
{/if}
