<script lang="ts">
	import { onMount } from 'svelte';
	import { canCreateRoles, canUpdateRoles, canDeleteRoles } from '$lib/stores/auth';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Switch } from '$lib/components/ui/switch';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';

	import { toast } from 'svelte-sonner';
	import {
		Plus, Trash2, Edit, Loader2, Check, X, ShieldAlert, KeyRound,
		Shield, Target, Save
	} from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { Role, Permission } from '$lib/proto/discopanel/v1/common_pb';
	import type { ScopeableObject, ResourceActions } from '$lib/proto/discopanel/v1/role_pb';
	import {
		CreateRoleRequestSchema,
		DeleteRoleRequestSchema,
		GetPermissionMatrixRequestSchema,
		UpdatePermissionsRequestSchema
	} from '$lib/proto/discopanel/v1/role_pb';

	type PermSection = 'global' | 'scoped';

	let roles = $state<Role[]>([]);
	let resourceActions = $state<ResourceActions[]>([]);
	let permissionMatrix = $state<Record<string, Permission[]>>({});
	let availableObjects = $state<ScopeableObject[]>([]);
	let loading = $state(true);
	let canCreate = $derived($canCreateRoles);
	let canUpdate = $derived($canUpdateRoles);
	let canDelete = $derived($canDeleteRoles);

	let showCreateDialog = $state(false);
	let showPermissionsDialog = $state(false);
	let editingRole = $state<Role | null>(null);
	let editingPermissions = $state<Record<string, boolean>>({});
	let savingPermissions = $state(false);
	let activeSection = $state<PermSection>('global');

	// Scoped permissions state
	let scopedPermissions = $state<{ resource: string; action: string; objectId: string; objectName: string }[]>([]);
	let scopedResource = $state('');

	let newRoleForm = $state({
		name: '',
		description: '',
		isDefault: false
	});

	const navItems: { id: PermSection; label: string; icon: typeof Shield }[] = [
		{ id: 'global', label: 'Global Permissions', icon: Shield },
		{ id: 'scoped', label: 'Scoped Permissions', icon: Target },
	];

	let scopeableResources = $derived(
		[...new Set(availableObjects.map(o => o.resource))]
	);

	// Map resource → scope source (e.g., "files" → "servers")
	let scopeSourceMap = $derived.by(() => {
		const map: Record<string, string> = {};
		for (const obj of availableObjects) {
			if (obj.scopeSource && !map[obj.resource]) {
				map[obj.resource] = obj.scopeSource;
			}
		}
		return map;
	});

	let scopedResourceActions = $derived(
		scopedResource
			? resourceActions.find(ra => ra.resource === scopedResource)?.actions ?? []
			: []
	);

	let filteredObjects = $derived(
		scopedResource
			? availableObjects.filter(o => o.resource === scopedResource)
			: []
	);

	let totalPermCount = $derived(
		Object.values(editingPermissions).filter(Boolean).length
	);

	let scopedCount = $derived(scopedPermissions.length);

	let allActions = $derived(
		[...new Set(resourceActions.flatMap(ra => ra.actions))].sort()
	);

	function hasFullAccess(roleName: string): boolean {
		const perms = permissionMatrix[roleName] || [];
		return perms.some(p => p.resource === '*' && p.action === '*');
	}

	function formatResourceName(resource: string): string {
		return resource.replace(/_/g, ' ');
	}

	function getResourcePermCount(resource: string): number {
		const ra = resourceActions.find(r => r.resource === resource);
		if (!ra) return 0;
		return ra.actions.filter(act => editingPermissions[`${resource}:${act}`]).length;
	}

	async function loadRoles() {
		loading = true;
		try {
			const matrixRequest = create(GetPermissionMatrixRequestSchema, { includeObjects: true });
			const [rolesResponse, matrixResponse] = await Promise.all([
				rpcClient.role.listRoles({}),
				rpcClient.role.getPermissionMatrix(matrixRequest)
			]);
			roles = rolesResponse.roles;
			resourceActions = matrixResponse.resourceActions;
			availableObjects = matrixResponse.availableObjects;

			const matrix: Record<string, Permission[]> = {};
			for (const [roleName, rolePerms] of Object.entries(matrixResponse.rolePermissions)) {
				matrix[roleName] = rolePerms.permissions;
			}
			permissionMatrix = matrix;
		} catch (error: unknown) {
			toast.error('Failed to load roles');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	async function createRole() {
		if (!newRoleForm.name) {
			toast.error('Role name is required');
			return;
		}

		try {
			const request = create(CreateRoleRequestSchema, {
				name: newRoleForm.name,
				description: newRoleForm.description,
				isDefault: newRoleForm.isDefault
			});
			await rpcClient.role.createRole(request);

			toast.success('Role created successfully');
			showCreateDialog = false;
			newRoleForm = { name: '', description: '', isDefault: false };
			await loadRoles();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create role');
		}
	}

	async function deleteRole(role: Role) {
		if (role.isSystem) {
			toast.error('Cannot delete system roles');
			return;
		}
		if (!confirm(`Are you sure you want to delete role "${role.name}"?`)) {
			return;
		}

		try {
			const request = create(DeleteRoleRequestSchema, { id: role.id });
			await rpcClient.role.deleteRole(request);

			toast.success('Role deleted successfully');
			await loadRoles();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete role');
		}
	}

	function openPermissionsDialog(role: Role) {
		editingRole = role;
		activeSection = 'global';
		scopedResource = '';

		const permMap: Record<string, boolean> = {};
		const scoped: typeof scopedPermissions = [];
		const rolePerms = permissionMatrix[role.name] || [];

		for (const perm of rolePerms) {
			if (perm.resource === '*' && perm.action === '*') {
				for (const ra of resourceActions) {
					for (const act of ra.actions) {
						permMap[`${ra.resource}:${act}`] = true;
					}
				}
			} else if (perm.objectId && perm.objectId !== '*') {
				const obj = availableObjects.find(o => o.id === perm.objectId);
				scoped.push({
					resource: perm.resource,
					action: perm.action,
					objectId: perm.objectId,
					objectName: obj?.name || perm.objectId
				});
			} else {
				permMap[`${perm.resource}:${perm.action}`] = true;
			}
		}
		editingPermissions = permMap;
		scopedPermissions = scoped;
		showPermissionsDialog = true;
	}

	async function savePermissions() {
		if (!editingRole) return;
		savingPermissions = true;

		const permissions: { resource: string; action: string; objectId: string }[] = [];

		for (const [key, enabled] of Object.entries(editingPermissions)) {
			if (enabled) {
				const [resource, action] = key.split(':');
				permissions.push({ resource, action, objectId: '*' });
			}
		}

		for (const sp of scopedPermissions) {
			permissions.push({
				resource: sp.resource,
				action: sp.action,
				objectId: sp.objectId
			});
		}

		try {
			const request = create(UpdatePermissionsRequestSchema, {
				roleName: editingRole.name,
				permissions
			});
			await rpcClient.role.updatePermissions(request);

			toast.success('Permissions updated');
			showPermissionsDialog = false;
			editingRole = null;
			await loadRoles();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to update permissions');
		} finally {
			savingPermissions = false;
		}
	}

	function togglePermission(key: string) {
		editingPermissions = {
			...editingPermissions,
			[key]: !editingPermissions[key]
		};
	}

	function toggleResourceAll(resource: string) {
		const ra = resourceActions.find(r => r.resource === resource);
		if (!ra) return;
		const allEnabled = ra.actions.every(act => editingPermissions[`${resource}:${act}`]);
		const updated = { ...editingPermissions };
		for (const act of ra.actions) {
			updated[`${resource}:${act}`] = !allEnabled;
		}
		editingPermissions = updated;
	}

	function isResourceAllChecked(resource: string): boolean {
		const ra = resourceActions.find(r => r.resource === resource);
		if (!ra) return false;
		return ra.actions.every(act => editingPermissions[`${resource}:${act}`]);
	}

	function isResourceIndeterminate(resource: string): boolean {
		const ra = resourceActions.find(r => r.resource === resource);
		if (!ra) return false;
		const checked = ra.actions.filter(act => editingPermissions[`${resource}:${act}`]).length;
		return checked > 0 && checked < ra.actions.length;
	}

	function isScopedChecked(resource: string, action: string, objectId: string): boolean {
		return scopedPermissions.some(
			sp => sp.resource === resource && sp.action === action && sp.objectId === objectId
		);
	}

	function toggleScopedPermission(resource: string, action: string, objectId: string, objectName: string) {
		const index = scopedPermissions.findIndex(
			sp => sp.resource === resource && sp.action === action && sp.objectId === objectId
		);
		if (index >= 0) {
			scopedPermissions = scopedPermissions.filter((_, i) => i !== index);
		} else {
			scopedPermissions = [...scopedPermissions, { resource, action, objectId, objectName }];
		}
	}

	onMount(() => {
		loadRoles();
	});
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<p class="text-sm text-muted-foreground">Manage roles and their permissions</p>
		{#if canCreate}
			<Button onclick={() => showCreateDialog = true}>
				<Plus class="mr-2 h-4 w-4" />
				Create Role
			</Button>
		{/if}
	</div>

	<Card>
		<CardContent>
			{#if loading}
				<div class="flex items-center justify-center py-16">
					<div class="text-center space-y-3">
						<Loader2 class="h-8 w-8 mx-auto animate-spin text-primary" />
						<div class="text-muted-foreground">Loading roles...</div>
					</div>
				</div>
			{:else}
				<Table>
					<TableHeader>
						<TableRow>
							<TableHead>Name</TableHead>
							<TableHead>Description</TableHead>
							<TableHead>Type</TableHead>
							<TableHead>Default</TableHead>
							<TableHead>Permissions</TableHead>
							{#if canDelete}
								<TableHead class="text-right">Actions</TableHead>
							{/if}
						</TableRow>
					</TableHeader>
					<TableBody>
						{#each roles as role (role.id)}
							<TableRow>
								<TableCell class="font-medium">{role.name}</TableCell>
								<TableCell class="text-muted-foreground">{role.description || '-'}</TableCell>
								<TableCell>
									{#if role.isSystem}
										<Badge variant="secondary">System</Badge>
									{:else}
										<Badge variant="outline">Custom</Badge>
									{/if}
								</TableCell>
								<TableCell>
									{#if role.isDefault}
										<Check class="h-4 w-4 text-green-500" />
									{:else}
										<X class="h-4 w-4 text-muted-foreground" />
									{/if}
								</TableCell>
								<TableCell>
									{#if hasFullAccess(role.name)}
										<Badge variant="destructive">Full Access</Badge>
									{:else if canUpdate}
										<Button size="sm" variant="outline" onclick={() => openPermissionsDialog(role)}>
											<Edit class="mr-1 h-3 w-3" />
											Edit ({role.permissions?.length || 0})
										</Button>
									{:else}
										<span class="text-sm text-muted-foreground">{role.permissions?.length || 0} permissions</span>
									{/if}
								</TableCell>
								{#if canDelete}
									<TableCell class="text-right">
										{#if !role.isSystem}
											<Button
												size="sm"
												variant="outline"
												onclick={() => deleteRole(role)}
											>
												<Trash2 class="h-4 w-4" />
											</Button>
										{/if}
									</TableCell>
								{/if}
							</TableRow>
						{/each}
					</TableBody>
				</Table>
			{/if}
		</CardContent>
	</Card>
</div>

<!-- Create Role Dialog -->
<Dialog open={showCreateDialog} onOpenChange={(open) => showCreateDialog = open}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New Role</DialogTitle>
			<DialogDescription>
				Create a custom role with specific permissions.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4">
			<div class="space-y-2">
				<Label for="role-name">Role Name</Label>
				<Input
					id="role-name"
					type="text"
					bind:value={newRoleForm.name}
					placeholder="e.g., moderator"
					required
				/>
			</div>
			<div class="space-y-2">
				<Label for="role-desc">Description</Label>
				<Input
					id="role-desc"
					type="text"
					bind:value={newRoleForm.description}
					placeholder="What this role is for"
				/>
			</div>
			<div class="flex items-center space-x-2">
				<Switch
					id="role-default"
					checked={newRoleForm.isDefault}
					onCheckedChange={(checked) => newRoleForm.isDefault = checked}
				/>
				<Label for="role-default">Assign to new users by default</Label>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => showCreateDialog = false}>Cancel</Button>
			<Button onclick={createRole}>Create Role</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<!-- Permissions Editor - Full-size Dialog with Sidebar -->
<Dialog open={showPermissionsDialog} onOpenChange={(open) => showPermissionsDialog = open}>
	<DialogContent class="max-w-6xl! w-[95vw]! h-[85vh]! p-0! gap-0! overflow-hidden flex flex-col" showCloseButton={false}>
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
							<h3 class="font-semibold truncate">{editingRole?.name}</h3>
							<p class="text-xs text-muted-foreground mt-0.5">
								{editingRole?.isSystem ? 'System role' : 'Custom role'}
							</p>
						</div>
					</div>
				</div>

				<!-- Navigation -->
				<nav class="flex-1 p-4 space-y-1">
					{#each navItems as item (item.id)}
						{@const Icon = item.icon}
						<button
							onclick={() => activeSection = item.id}
							class="w-full flex items-center gap-3 px-4 py-3 rounded-lg text-left transition-colors {activeSection === item.id
								? 'bg-primary text-primary-foreground'
								: 'hover:bg-muted text-muted-foreground hover:text-foreground'}"
						>
							<Icon class="h-5 w-5" />
							<span class="font-medium flex-1">{item.label}</span>
							{#if item.id === 'global'}
								<span class="text-xs opacity-75">{totalPermCount}</span>
							{:else if item.id === 'scoped'}
								<span class="text-xs opacity-75">{scopedCount}</span>
							{/if}
						</button>
					{/each}
				</nav>

				<!-- Sidebar Footer -->
				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-1">Permission Model</p>
						<p class="text-xs text-muted-foreground">
							Global permissions apply to all objects. Scoped permissions target specific objects like individual servers.
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
							{#if activeSection === 'global'}Global Permissions
							{:else}Scoped Permissions
							{/if}
						</h2>
						<p class="text-muted-foreground mt-1">
							{#if activeSection === 'global'}Toggle access to resources and their actions
							{:else}Grant access to specific objects instead of all objects of a type
							{/if}
						</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => showPermissionsDialog = false} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<!-- Scrollable Content Area -->
				<div class="flex-1 overflow-y-auto p-8">
					{#if activeSection === 'global'}
						<!-- Global Permissions Matrix -->
						<div class="overflow-x-auto border rounded-lg">
							<Table>
								<TableHeader>
									<TableRow class="bg-muted/50">
										<TableHead class="sticky left-0 bg-muted/50 z-10 w-50 border-r">Resource</TableHead>
										{#each allActions as action (action)}
											<TableHead class="text-center px-3">
												<span class="capitalize text-xs font-medium">{action}</span>
											</TableHead>
										{/each}
									</TableRow>
								</TableHeader>
								<TableBody>
									{#each resourceActions as ra (ra.resource)}
										{@const count = getResourcePermCount(ra.resource)}
										{@const total = ra.actions.length}
										<TableRow class="hover:bg-muted/30">
											<TableCell class="sticky left-0 bg-background z-10 font-medium border-r">
												<div class="flex items-center gap-2">
													<Checkbox
														checked={isResourceAllChecked(ra.resource)}
														indeterminate={isResourceIndeterminate(ra.resource)}
														onCheckedChange={() => toggleResourceAll(ra.resource)}
													/>
													<span class="capitalize text-sm">{formatResourceName(ra.resource)}</span>
													{#if count > 0}
														<Badge variant="secondary" class="text-[10px] ml-auto px-1.5 py-0">{count}/{total}</Badge>
													{/if}
												</div>
											</TableCell>
											{#each allActions as action (action)}
												{@const key = `${ra.resource}:${action}`}
												{@const hasAction = ra.actions.includes(action)}
												{@const checked = hasAction && (editingPermissions[key] || false)}
												<TableCell class="text-center px-3">
													{#if hasAction}
														<div class="flex justify-center">
															<Checkbox
																{checked}
																onCheckedChange={() => togglePermission(key)}
															/>
														</div>
													{:else}
														<span class="text-muted-foreground/20">—</span>
													{/if}
												</TableCell>
											{/each}
										</TableRow>
									{/each}
								</TableBody>
							</Table>
						</div>

					{:else}
						<!-- Scoped Permissions -->
						{#if scopeableResources.length === 0}
							<div class="flex flex-col items-center justify-center py-16 text-center border rounded-xl border-dashed">
								<Target class="h-12 w-12 text-muted-foreground/50 mb-4" />
								<h3 class="font-medium mb-1">No scopeable resources</h3>
								<p class="text-sm text-muted-foreground max-w-sm">
									No objects exist yet to scope permissions to. Create resources like servers first.
								</p>
							</div>
						{:else}
							<div class="space-y-6">
								<!-- Resource type picker -->
								<div class="flex flex-wrap gap-2">
									{#each scopeableResources as res (res)}
										{@const objectCount = availableObjects.filter(o => o.resource === res).length}
										{@const source = scopeSourceMap[res]}
										{@const isForeign = source && source !== res}
										<button
											onclick={() => scopedResource = scopedResource === res ? '' : res}
											class="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg text-sm font-medium capitalize transition-colors {scopedResource === res
												? 'bg-primary text-primary-foreground'
												: 'bg-muted hover:bg-muted/80 text-muted-foreground hover:text-foreground'}"
										>
											{formatResourceName(res)}
											{#if isForeign}
												<span class="text-xs opacity-60 normal-case">via {formatResourceName(source)}</span>
											{/if}
											<span class="text-xs opacity-75">({objectCount})</span>
										</button>
									{/each}
								</div>

								{#if scopedResource}
									{@const activeSource = scopeSourceMap[scopedResource]}
									{@const activeForeign = activeSource && activeSource !== scopedResource}
									{#if filteredObjects.length === 0}
										<div class="flex flex-col items-center justify-center py-12 text-center border rounded-xl border-dashed">
											<ShieldAlert class="h-10 w-10 text-muted-foreground/50 mb-3" />
											<h3 class="font-medium mb-1">No {formatResourceName(activeForeign ? activeSource : scopedResource)}</h3>
											<p class="text-sm text-muted-foreground">
												{#if activeForeign}
													No {formatResourceName(activeSource)} exist to scope {formatResourceName(scopedResource)} permissions.
												{:else}
													No {formatResourceName(scopedResource)} exist yet to scope permissions to.
												{/if}
											</p>
										</div>
									{:else}
										<div class="overflow-x-auto border rounded-lg">
											<Table>
												<TableHeader>
													<TableRow class="bg-muted/50">
														<TableHead class="sticky left-0 bg-muted/50 z-10 w-50 border-r">
																<span class="capitalize">{formatResourceName(activeForeign ? activeSource : scopedResource)}</span>
																{#if activeForeign}
																	<div class="text-[10px] text-muted-foreground font-normal normal-case">scoping {formatResourceName(scopedResource)}</div>
																{/if}
															</TableHead>
														{#each scopedResourceActions as action (action)}
															{@const coveredByGlobal = editingPermissions[`${scopedResource}:${action}`] || false}
															<TableHead class="text-center px-3">
																<span class="capitalize text-xs font-medium {coveredByGlobal ? 'opacity-50' : ''}">{action}</span>
																{#if coveredByGlobal}
																	<div class="text-[9px] text-muted-foreground font-normal">(global)</div>
																{/if}
															</TableHead>
														{/each}
													</TableRow>
												</TableHeader>
												<TableBody>
													{#each filteredObjects as obj (obj.id)}
														<TableRow class="hover:bg-muted/30">
															<TableCell class="sticky left-0 bg-background z-10 font-medium border-r">
																<span class="text-sm">{obj.name}</span>
															</TableCell>
															{#each scopedResourceActions as action (action)}
																{@const coveredByGlobal = editingPermissions[`${scopedResource}:${action}`] || false}
																{@const checked = isScopedChecked(scopedResource, action, obj.id)}
																<TableCell class="text-center px-3">
																	<div class="flex justify-center {coveredByGlobal ? 'opacity-40' : ''}">
																		<Checkbox
																			checked={checked || coveredByGlobal}
																			disabled={coveredByGlobal}
																			onCheckedChange={() => toggleScopedPermission(scopedResource, action, obj.id, obj.name)}
																		/>
																	</div>
																</TableCell>
															{/each}
														</TableRow>
													{/each}
												</TableBody>
											</Table>
										</div>
									{/if}
								{:else}
									<div class="flex flex-col items-center justify-center py-12 text-center border rounded-xl border-dashed">
										<Target class="h-10 w-10 text-muted-foreground/50 mb-3" />
										<p class="text-sm text-muted-foreground">Select a resource type above to manage scoped permissions</p>
									</div>
								{/if}
							</div>
						{/if}
					{/if}
				</div>

				<!-- Footer -->
				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					<Button variant="outline" onclick={() => showPermissionsDialog = false} class="h-11 px-6">
						Cancel
					</Button>
					<Button
						onclick={savePermissions}
						disabled={savingPermissions}
						class="h-11 px-8 gap-2"
					>
						{#if savingPermissions}
							<Loader2 class="h-4 w-4 animate-spin" />
							Saving...
						{:else}
							<Save class="h-4 w-4" />
							Save Permissions
						{/if}
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>
