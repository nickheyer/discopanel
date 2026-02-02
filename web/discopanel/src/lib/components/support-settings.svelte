<script lang="ts">
	import { onMount } from 'svelte';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Label } from '$lib/components/ui/label';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import {
		Download,
		AlertCircle,
		CheckCircle2,
		Loader2,
		FileArchive,
		Database,
		ScrollText,
		Send,
		Copy,
		ExternalLink,
		User,
		Mail,
		MessageSquare,
		Github,
		Server,
		ChevronDown,
		ChevronUp
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { rpcClient } from '$lib/api/rpc-client';
	import { copyToClipboard } from '$lib/utils/clipboard';
	import { serversStore } from '$lib/stores/servers';
	import type { Server as ServerType } from '$lib/proto/discopanel/v1/common_pb';

	let generating = $state(false);
	let uploading = $state(false);
	let bundlePath = $state<string | null>(null);
	let referenceId = $state<string | null>(null);
	let discordUsername = $state('');
	let email = $state('');
	let githubUsername = $state('');
	let issueDescription = $state('');
	let stepsToReproduce = $state('');

	// Server selection
	let servers = $state<ServerType[]>([]);
	let selectedServerIds = $state<Set<string>>(new Set());
	let serverSectionExpanded = $state(false);
	let loadingServers = $state(true);

	onMount(async () => {
		try {
			servers = await serversStore.fetchServers(true);
		} catch (error) {
			console.error('Failed to load servers:', error);
		} finally {
			loadingServers = false;
		}
	});

	function toggleServer(serverId: string) {
		const newSet = new Set(selectedServerIds);
		if (newSet.has(serverId)) {
			newSet.delete(serverId);
		} else {
			newSet.add(serverId);
		}
		selectedServerIds = newSet;
	}

	function selectAllServers() {
		selectedServerIds = new Set(servers.map(s => s.id));
	}

	function clearServerSelection() {
		selectedServerIds = new Set();
	}

	async function generateBundle(upload: boolean = false) {
		if (upload) {
			uploading = true;
		} else {
			generating = true;
		}

		bundlePath = null;
		referenceId = null;

		const serverIds = Array.from(selectedServerIds);

		try {
			if (upload) {
				const response = await rpcClient.support.uploadSupportBundle({
					includeLogs: true,
					includeConfigs: true,
					includeSystemInfo: true,
					serverIds,
					discordUsername: discordUsername.trim(),
					email: email.trim(),
					githubUsername: githubUsername.trim(),
					issueDescription: issueDescription.trim(),
					stepsToReproduce: stepsToReproduce.trim()
				});

				if (response.success && response.referenceId) {
					referenceId = response.referenceId;
					discordUsername = '';
					email = '';
					githubUsername = '';
					issueDescription = '';
					stepsToReproduce = '';
					toast.success('Support bundle uploaded successfully!', {
						description: 'Save your reference ID for support requests.',
					});
				} else {
					toast.error('Failed to upload support bundle', {
						description: response.message || 'Unknown error occurred',
					});
				}
			} else {
				const response = await rpcClient.support.generateSupportBundle({
					includeLogs: true,
					includeConfigs: true,
					includeSystemInfo: true,
					serverIds
				});

				if (response.bundleId) {
					bundlePath = response.bundleId;
					toast.success('Support bundle generated!', {
						description: 'Click the download button to save the bundle.',
					});
				} else {
					toast.error('Failed to generate support bundle', {
						description: response.message,
					});
				}
			}
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Unknown error occurred';
			const action = upload ? 'upload' : 'generate';
			toast.error(`Failed to ${action} support bundle`, {
				description: message,
			});
		} finally {
			generating = false;
			uploading = false;
		}
	}

	async function downloadBundle() {
		if (!bundlePath) return;

		try {
			const response = await rpcClient.support.downloadSupportBundle({
				bundleId: bundlePath
			});
			// Create download link
			const blob = new Blob([
				new Uint8Array(response.content)
			], { type: response.mimeType });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = response.filename;
			a.click();
			URL.revokeObjectURL(url);
			// Clear the bundle path after download
			bundlePath = null;
			toast.success('Support bundle downloaded!');
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Unknown error occurred';
			toast.error('Failed to download support bundle', {
				description: message,
			});
		}
	}

	async function copyReferenceId() {
		if (!referenceId) return;
		const success = await copyToClipboard(referenceId);
		if (success) {
			toast.success('Reference ID copied to clipboard!');
		} else {
			toast.error('Failed to copy to clipboard');
		}
	}
</script>

<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
	<CardHeader class="relative pb-6">
		<div class="flex items-center justify-between">
			<div>
				<CardTitle class="text-2xl font-semibold">Support Tools</CardTitle>
				<CardDescription class="text-base mt-2">
					Generate and upload support bundles to help troubleshoot issues with DiscoPanel.
				</CardDescription>
			</div>
		</div>
	</CardHeader>
	<CardContent class="flex flex-col space-y-4 w-full">
		<!-- Support Bundle Info -->
		<div class="w-full rounded-xl bg-gradient-to-br from-primary/5 via-primary/3 to-transparent border border-primary/20 p-6">
			<div class="flex items-start gap-3 mb-4">
				<div class="rounded-lg bg-primary/10 p-2.5">
					<AlertCircle class="h-5 w-5 text-primary" />
				</div>
				<div>
					<h3 class="font-semibold text-base">What's included in your support bundle?</h3>
					<p class="text-sm text-muted-foreground mt-1">
						We collect essential diagnostic data to help resolve issues quickly
					</p>
				</div>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<!-- Application Logs Card -->
				<div class="group relative rounded-lg border border-border/50 bg-card/50 backdrop-blur-sm p-4  transition-all duration-200">
					<div class="absolute inset-0 rounded-lg bg-gradient-to-br from-primary/5 to-transparent opacity-0"></div>
					<div class="relative space-y-2">
						<div class="flex items-center gap-2">
							<div class="rounded-md bg-primary/10 p-1.5">
								<ScrollText class="h-4 w-4 text-primary" />
							</div>
							<h4 class="font-semibold text-sm">Application Logs</h4>
						</div>
						<p class="text-xs text-muted-foreground leading-relaxed">
							Recent log entries and error messages to track down issues
						</p>
					</div>
				</div>

				<!-- Database Snapshot Card -->
				<div class="group relative rounded-lg border border-border/50 bg-card/50 backdrop-blur-sm p-4 transition-all duration-200">
					<div class="absolute inset-0 rounded-lg bg-gradient-to-br from-primary/5 to-transparent opacity-0"></div>
					<div class="relative space-y-2">
						<div class="flex items-center gap-2">
							<div class="rounded-md bg-primary/10 p-1.5">
								<Database class="h-4 w-4 text-primary" />
							</div>
							<h4 class="font-semibold text-sm">Database Snapshot</h4>
						</div>
						<p class="text-xs text-muted-foreground leading-relaxed">
							Current configuration and server data for analysis
						</p>
					</div>
				</div>

				<!-- System Information Card -->
				<div class="group relative rounded-lg border border-border/50 bg-card/50 backdrop-blur-sm p-4  transition-all duration-200">
					<div class="absolute inset-0 rounded-lg bg-gradient-to-br from-primary/5 to-transparent opacity-0"></div>
					<div class="relative space-y-2">
						<div class="flex items-center gap-2">
							<div class="rounded-md bg-primary/10 p-1.5">
								<FileArchive class="h-4 w-4 text-primary" />
							</div>
							<h4 class="font-semibold text-sm">System Information</h4>
						</div>
						<p class="text-xs text-muted-foreground leading-relaxed">
							Version and environment details for compatibility checks
						</p>
					</div>
				</div>
			</div>
		</div>

		<!-- Server Selection -->
		<div class="w-full rounded-xl border border-border/50 bg-muted/20 p-6">
			<button
				type="button"
				class="w-full flex items-center justify-between cursor-pointer"
				onclick={() => serverSectionExpanded = !serverSectionExpanded}
			>
				<div class="flex items-start gap-3">
					<div class="rounded-lg bg-primary/10 p-2.5">
						<Server class="h-5 w-5 text-primary" />
					</div>
					<div class="text-left">
						<h3 class="font-semibold text-base">Server Selection</h3>
						<p class="text-sm text-muted-foreground mt-1">
							{#if selectedServerIds.size === 0}
								Include all servers (default)
							{:else}
								{selectedServerIds.size} server{selectedServerIds.size === 1 ? '' : 's'} selected
							{/if}
						</p>
					</div>
				</div>
				<div class="rounded-md bg-muted p-1.5">
					{#if serverSectionExpanded}
						<ChevronUp class="h-4 w-4 text-muted-foreground" />
					{:else}
						<ChevronDown class="h-4 w-4 text-muted-foreground" />
					{/if}
				</div>
			</button>

			{#if serverSectionExpanded}
				<div class="mt-4 pt-4 border-t border-border/50">
					{#if loadingServers}
						<div class="flex items-center justify-center py-4">
							<Loader2 class="h-5 w-5 animate-spin text-muted-foreground" />
							<span class="ml-2 text-sm text-muted-foreground">Loading servers...</span>
						</div>
					{:else if servers.length === 0}
						<p class="text-sm text-muted-foreground text-center py-4">
							No servers found. All available data will be included.
						</p>
					{:else}
						<div class="space-y-3">
							<div class="flex items-center justify-between">
								<p class="text-xs text-muted-foreground">
									Select specific servers to include their logs and configurations
								</p>
								<div class="flex gap-2">
									<Button
										variant="ghost"
										size="sm"
										class="h-7 text-xs"
										onclick={selectAllServers}
									>
										Select All
									</Button>
									<Button
										variant="ghost"
										size="sm"
										class="h-7 text-xs"
										onclick={clearServerSelection}
										disabled={selectedServerIds.size === 0}
									>
										Clear
									</Button>
								</div>
							</div>
							<div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2">
								{#each servers as server (server.id)}
									<button
										type="button"
										class="flex items-center gap-3 p-3 rounded-lg border border-border/50 bg-card/50 hover:bg-card/80 transition-colors cursor-pointer text-left {selectedServerIds.has(server.id) ? 'border-primary/50 bg-primary/5' : ''}"
										onclick={() => toggleServer(server.id)}
									>
										<Checkbox
											checked={selectedServerIds.has(server.id)}
											class="pointer-events-none"
										/>
										<div class="min-w-0 flex-1">
											<p class="text-sm font-medium truncate">{server.name}</p>
											<p class="text-xs text-muted-foreground truncate">
												{server.mcVersion || 'Unknown version'}
											</p>
										</div>
									</button>
								{/each}
							</div>
						</div>
					{/if}
				</div>
			{/if}
		</div>

		<!-- User Contact & Issue Information -->
		<div class="w-full rounded-xl border border-border/50 bg-muted/20 p-6">
			<div class="flex items-start gap-3 mb-4">
				<div class="rounded-lg bg-primary/10 p-2.5">
					<MessageSquare class="h-5 w-5 text-primary" />
				</div>
				<div>
					<h3 class="font-semibold text-base">Contact & Issue Details</h3>
					<p class="text-sm text-muted-foreground mt-1">
						Optional information to help us respond to your support request
					</p>
				</div>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
				<!-- Discord Username -->
				<div class="space-y-2">
					<Label for="discord" class="text-sm font-medium flex items-center gap-2">
						<User class="h-3.5 w-3.5" />
						Discord Username
					</Label>
					<Input
						id="discord"
						type="text"
						placeholder="username#1234"
						bind:value={discordUsername}
						class="h-9"
					/>
				</div>

				<!-- Email -->
				<div class="space-y-2">
					<Label for="email" class="text-sm font-medium flex items-center gap-2">
						<Mail class="h-3.5 w-3.5" />
						Email
					</Label>
					<Input
						id="email"
						type="email"
						placeholder="you@example.com"
						bind:value={email}
						class="h-9"
					/>
				</div>

				<!-- GitHub Username -->
				<div class="space-y-2">
					<Label for="github" class="text-sm font-medium flex items-center gap-2">
						<Github class="h-3.5 w-3.5" />
						GitHub Username
					</Label>
					<Input
						id="github"
						type="text"
						placeholder="username"
						bind:value={githubUsername}
						class="h-9"
					/>
				</div>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<!-- Issue Description -->
				<div class="space-y-2">
					<Label for="description" class="text-sm font-medium">Issue Description</Label>
					<Textarea
						id="description"
						placeholder="Describe the issue you're experiencing..."
						bind:value={issueDescription}
						rows={3}
						class="resize-none"
					/>
				</div>

				<!-- Steps to Reproduce -->
				<div class="space-y-2">
					<Label for="steps" class="text-sm font-medium">Steps to Reproduce</Label>
					<Textarea
						id="steps"
						placeholder="1. Go to...&#10;2. Click on...&#10;3. See error..."
						bind:value={stepsToReproduce}
						rows={3}
						class="resize-none"
					/>
				</div>
			</div>
		</div>

		<!-- Action Buttons -->
		<div class="space-y-4">
			<div class="flex flex-col sm:flex-row gap-3">
				<!-- Generate for Download -->
				<div class="flex-1">
					<Button
						onclick={() => generateBundle(false)}
						disabled={generating || uploading}
						class="w-full h-auto py-4 px-6 relative overflow-hidden group"
						variant="outline"
					>
						<div class="absolute inset-0 bg-gradient-to-r from-primary/10 to-primary/5 opacity-0 group-hover:opacity-100 transition-opacity"></div>
						<div class="relative flex items-center justify-center gap-3">
							{#if generating && !uploading}
								<Loader2 class="h-5 w-5 animate-spin" />
								<div class="text-left">
									<div class="font-semibold">Generating Bundle...</div>
									<div class="text-xs text-muted-foreground">Please wait</div>
								</div>
							{:else}
								<Download class="h-5 w-5" />
								<div class="text-left">
									<div class="font-semibold">Generate and download</div>
									<div class="text-xs text-muted-foreground">Create a local copy of support data</div>
								</div>
							{/if}
						</div>
					</Button>
				</div>

				<!-- Upload to Support -->
				<div class="flex-1">
					<Button
						onclick={() => generateBundle(true)}
						disabled={generating || uploading}
						class="w-full h-auto py-4 px-6 relative overflow-hidden group"
						variant="outline"
					>
						<div class="absolute inset-0 bg-gradient-to-r from-primary/10 to-primary/5 opacity-0 group-hover:opacity-100 transition-opacity"></div>
						<div class="relative flex items-center justify-center gap-3">
							{#if uploading}
								<Loader2 class="h-5 w-5 animate-spin" />
								<div class="text-left">
									<div class="font-semibold">Uploading...</div>
									<div class="text-xs text-muted-foreground">Sending to support</div>
								</div>
							{:else}
								<Send class="h-5 w-5" />
								<div class="text-left">
									<div class="font-semibold">Upload to Support</div>
									<div class="text-xs text-muted-foreground">Send directly to support team</div>
								</div>
							{/if}
						</div>
					</Button>
				</div>
			</div>

			<!-- Download Ready Alert -->
			{#if bundlePath}
				<Alert class="border-green-500/50 bg-green-500/10">
					<CheckCircle2 class="h-4 w-4 text-green-600 dark:text-green-400" />
					<div class="flex items-center justify-between">
						<div>
							<AlertDescription class="font-medium text-green-900 dark:text-green-100">
								Support bundle ready for download
							</AlertDescription>
							<AlertDescription class="text-xs text-muted-foreground mt-1">
								Bundle will be deleted after download
							</AlertDescription>
						</div>
						<Button
							onclick={downloadBundle}
							size="sm"
							variant="outline"
							class="ml-4 border-green-500/50 hover:bg-green-500/10"
						>
							<Download class="h-4 w-4 mr-2" />
							Download Now
						</Button>
					</div>
				</Alert>
			{/if}

			<!-- Upload Success Alert -->
			{#if referenceId}
				<Alert class="border-green-500/50 bg-green-500/10">
					<CheckCircle2 class="h-4 w-4 text-green-600 dark:text-green-400" />
					<AlertDescription>
						<div class="space-y-3">
							<div class="font-medium text-green-900 dark:text-green-100">
								Support bundle uploaded successfully!
							</div>

							<!-- Reference ID Display -->
							<div class="bg-background/50 rounded-lg p-3 space-y-2">
								<div class="text-xs font-medium text-muted-foreground uppercase tracking-wide">
									Reference ID
								</div>
								<div class="flex items-center gap-2">
									<code class="flex-1 font-mono text-sm bg-background px-3 py-2 rounded border border-border">
										{referenceId}
									</code>
									<Button
										onclick={copyReferenceId}
										size="sm"
										variant="outline"
										class="flex-shrink-0"
									>
										<Copy class="h-3 w-3" />
									</Button>
								</div>
							</div>

							<div class="space-y-2 text-sm text-muted-foreground">
								<p class="font-medium">Please include this reference ID when:</p>
								<ul class="list-disc list-inside space-y-1 ml-2">
									<li>Requesting help in our Discord server</li>
									<li>Creating an issue on GitHub</li>
									<li>Contacting support directly</li>
								</ul>
							</div>

							<div class="pt-2 border-t border-border/50">
								<a
									href="https://discopanel.app"
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 text-xs text-primary hover:underline"
								>
									For more info and links to Discord/Github, please visit our site!
									<ExternalLink class="h-3 w-3" />
								</a>
							</div>
						</div>
					</AlertDescription>
				</Alert>
			{/if}
		</div>

		<!-- Privacy Notice -->
		<div class="rounded-lg border border-border/50 bg-muted/30 p-4">
			<div class="flex gap-3">
				<AlertCircle class="h-4 w-4 text-muted-foreground mt-0.5 flex-shrink-0" />
				<div class="space-y-1 text-sm text-muted-foreground">
					<p class="font-medium">Privacy Notice</p>
					<p class="text-xs leading-relaxed">
						Support bundles contain server configurations, logs, and database information.
						While we take privacy seriously and handle your data securely, please review the
						bundle contents before uploading if you have sensitive information.
					</p>
				</div>
			</div>
		</div>
	</CardContent>
</Card>
