<script lang="ts">
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Badge } from '$lib/components/ui/badge';
	import {
		Download,
		HelpCircle,
		AlertCircle,
		CheckCircle2,
		Loader2,
		FileArchive,
		Database,
		ScrollText,
		Send,
		Copy,
		ExternalLink
	} from '@lucide/svelte';
	import { toast } from 'svelte-sonner';
	import { rpcClient } from '$lib/api/rpc-client';

	let generating = $state(false);
	let uploading = $state(false);
	let bundlePath = $state<string | null>(null);
	let referenceId = $state<string | null>(null);

	async function generateBundle(upload: boolean = false) {
		if (upload) {
			uploading = true;
		} else {
			generating = true;
		}

		bundlePath = null;
		referenceId = null;

		try {
			const response = await rpcClient.support.generateSupportBundle({
				includeLogs: true,
				includeConfigs: true,
				includeSystemInfo: true,
				serverIds: []
			});

			if (response.bundleId) {
				if (upload) {
					// For upload, the bundle ID serves as reference
					referenceId = response.bundleId;
					toast.success('Support bundle uploaded successfully!', {
						description: 'Save your reference ID for support requests.',
					});
				} else {
					bundlePath = response.bundleId;
					toast.success('Support bundle generated!', {
						description: 'Click the download button to save the bundle.',
					});
				}
			} else {
				toast.error('Failed to generate support bundle', {
					description: response.message,
				});
			}
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Unknown error occurred';
			toast.error('Failed to generate support bundle', {
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
			// Create a download link
			const blob = new Blob([response.content], { type: response.mimeType });
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

		try {
			await navigator.clipboard.writeText(referenceId);
			toast.success('Reference ID copied to clipboard!');
		} catch (error) { // Fallback for jank browsers
			const textArea = document.createElement('textarea');
			textArea.value = referenceId;
			document.body.appendChild(textArea);
			textArea.select();
			document.execCommand('copy');
			document.body.removeChild(textArea);
			toast.success('Reference ID copied to clipboard!');
		}
	}
</script>

<Card class="relative overflow-hidden border-2 hover:border-primary/50 transition-all duration-300 hover:shadow-2xl bg-gradient-to-br from-card to-card/80">
	<div class="absolute inset-0 bg-gradient-to-br from-primary/10 via-transparent to-transparent opacity-0 hover:opacity-100 transition-opacity duration-300"></div>
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