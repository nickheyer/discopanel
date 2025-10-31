<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Plus, X, AlertCircle } from '@lucide/svelte';
	import type { AdditionalPort } from '$lib/api/types';

	interface Props {
		ports?: AdditionalPort[];
		disabled?: boolean;
		usedPorts?: Record<number, boolean>;
		onchange?: (ports: AdditionalPort[]) => void;
	}

	let { ports = $bindable([]), disabled = false, usedPorts = {}, onchange }: Props = $props();

	let portErrors = $state<Record<number, string>>({});

	function addPort() {
		const newPort: AdditionalPort = {
			name: '',
			container_port: findNextAvailablePort(),
			host_port: findNextAvailablePort(),
			protocol: 'tcp'
		};
		ports = [...ports, newPort];
		onchange?.(ports);
	}

	function removePort(index: number) {
		ports = ports.filter((_, i) => i !== index);
		// Clear any errors for this port
		delete portErrors[index];
		onchange?.(ports);
	}

	function updatePort(index: number, field: keyof AdditionalPort, value: any) {
		ports[index] = {
			...ports[index],
			[field]: value
		};

		// Validate port if it's a host_port change
		if (field === 'host_port') {
			const port = Number(value);
			if (port && usedPorts[port]) {
				portErrors[index] = `Port ${port} is already in use`;
			} else if (port < 1 || port > 65535) {
				portErrors[index] = 'Port must be between 1 and 65535';
			} else {
				// Check for duplicates within additional ports
				const hasDuplicate = ports.some((p, i) =>
					i !== index && p.host_port === port && p.protocol === ports[index].protocol
				);
				if (hasDuplicate) {
					portErrors[index] = `Duplicate port ${port}/${ports[index].protocol}`;
				} else {
					delete portErrors[index];
				}
			}
		}

		onchange?.(ports);
	}

	// Find next available port
	function findNextAvailablePort(startFrom: number = 25566): number {
		let port = startFrom;
		while (port <= 65535) {
			if (!usedPorts[port] && !ports.some(p => p.host_port === port)) {
				return port;
			}
			port++;
		}
		return 25566; // Fallback
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<Label class="text-sm font-medium">Additional Ports</Label>
			<p class="text-xs text-muted-foreground mt-1">
				Configure extra ports for mods, plugins, or services (e.g., BlueMap, voice chat, dynmap)
			</p>
		</div>
		<Button
			type="button"
			variant="outline"
			size="sm"
			onclick={addPort}
			disabled={disabled}
			class="h-8 gap-1"
		>
			<Plus class="h-3 w-3" />
			Add Port
		</Button>
	</div>

	{#if ports.length > 0}
		<div class="rounded-lg border bg-muted/10 p-3">
			<div class="space-y-3">
				<!-- Headers -->
				<div class="grid grid-cols-12 gap-2 px-1 text-xs font-medium text-muted-foreground">
					<div class="col-span-4">Name/Description</div>
					<div class="col-span-2">Container Port</div>
					<div class="col-span-2">Host Port</div>
					<div class="col-span-2">Protocol</div>
					<div class="col-span-2"></div>
				</div>

				<!-- Port entries -->
				{#each ports as port, index}
					<div class="space-y-2">
						<div class="grid grid-cols-12 gap-2 items-center">
							<div class="col-span-4">
								<Input
									type="text"
									placeholder="e.g., BlueMap Web"
									bind:value={port.name}
									disabled={disabled}
									onchange={() => updatePort(index, 'name', port.name)}
									class="h-8 text-xs"
								/>
							</div>
							<div class="col-span-2">
								<Input
									type="number"
									min="1"
									max="65535"
									placeholder="8100"
									bind:value={port.container_port}
									disabled={disabled}
									onchange={() => updatePort(index, 'container_port', port.container_port)}
									class="h-8 text-xs"
								/>
							</div>
							<div class="col-span-2">
								<Input
									type="number"
									min="1"
									max="65535"
									placeholder="8100"
									bind:value={port.host_port}
									disabled={disabled}
									onchange={() => updatePort(index, 'host_port', port.host_port)}
									class="h-8 text-xs {portErrors[index] ? 'border-destructive' : ''}"
								/>
							</div>
							<div class="col-span-2">
								<Select
									type="single"
									value={port.protocol}
									onValueChange={(v) => updatePort(index, 'protocol', v)}
									disabled={disabled}
								>
									<SelectTrigger class="h-8 text-xs">
										<span>{port.protocol.toUpperCase()}</span>
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="tcp">TCP</SelectItem>
										<SelectItem value="udp">UDP</SelectItem>
									</SelectContent>
								</Select>
							</div>
							<div class="col-span-2 flex justify-end">
								<Button
									type="button"
									variant="ghost"
									size="icon"
									onclick={() => removePort(index)}
									disabled={disabled}
									class="h-8 w-8 hover:bg-destructive/10 hover:text-destructive"
								>
									<X class="h-3 w-3" />
								</Button>
							</div>
						</div>

						{#if portErrors[index]}
							<div class="flex items-center gap-2 text-destructive pl-1">
								<AlertCircle class="h-3 w-3" />
								<span class="text-xs">{portErrors[index]}</span>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		</div>
	{:else}
		<div class="rounded-lg border border-dashed p-4">
			<p class="text-sm text-muted-foreground text-center">
				No additional ports configured. Click "Add Port" to expose extra ports for mods or services.
			</p>
		</div>
	{/if}
</div>