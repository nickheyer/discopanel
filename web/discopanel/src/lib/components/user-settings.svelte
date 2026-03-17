<script lang="ts">
	import { onMount } from 'svelte';
	import { authStore, canCreateUsers, canUpdateUsers, canDeleteUsers } from '$lib/stores/auth';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { Dialog, DialogContent } from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Users, UserPlus, Trash2, Edit, Loader2, TicketPlus, Copy, Check, X, Link, Shield, Clock, Hash, Lock, Save, KeyRound, Mail, UserCog } from '@lucide/svelte';
	import { Switch } from '$lib/components/ui/switch';
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { User, Role } from '$lib/proto/discopanel/v1/common_pb';
	import type { RegistrationInvite } from '$lib/proto/discopanel/v1/auth_pb';
	import { CreateUserRequestSchema, UpdateUserRequestSchema, DeleteUserRequestSchema } from '$lib/proto/discopanel/v1/user_pb';
	import { CreateInviteRequestSchema, DeleteInviteRequestSchema } from '$lib/proto/discopanel/v1/auth_pb';
	import { getRoleBadgeVariant } from '$lib/utils/role-colors';

	let users = $state<User[]>([]);
	let availableRoles = $state<Role[]>([]);
	let loading = $state(true);
	let canCreate = $derived($canCreateUsers);
	let canUpdate = $derived($canUpdateUsers);
	let canDelete = $derived($canDeleteUsers);
	let showCreateDialog = $state(false);
	let showEditDialog = $state(false);
	let editingUser = $state<User | null>(null);

	let newUserForm = $state({
		username: '',
		email: '',
		password: '',
		roles: [] as string[]
	});

	let editUserForm = $state({
		email: '',
		roles: [] as string[],
		isActive: true
	});

	// Invite state
	let invites = $state<RegistrationInvite[]>([]);
	let invitesLoading = $state(false);
	let showCreateInviteDialog = $state(false);
	let creatingInvite = $state(false);

	let newInviteForm = $state({
		description: '',
		roles: [] as string[],
		maxUses: null as number | null,
		pin: '',
		expiresValue: null as number | null,
		expiresUnit: 'hours' as 'hours' | 'days' | 'weeks',
	});

	function getInviteStatus(invite: RegistrationInvite): 'active' | 'expired' | 'exhausted' {
		if (invite.expiresAt && new Date(Number(invite.expiresAt.seconds) * 1000) < new Date()) return 'expired';
		if (invite.maxUses > 0 && invite.useCount >= invite.maxUses) return 'exhausted';
		return 'active';
	}

	function getStatusVariant(status: string): 'default' | 'secondary' | 'destructive' | 'outline' {
		if (status === 'active') return 'default';
		if (status === 'expired') return 'secondary';
		return 'destructive';
	}

	async function loadUsers() {
		loading = true;
		try {
			const [usersResponse, rolesResponse] = await Promise.all([
				rpcClient.user.listUsers({}),
				rpcClient.role.listRoles({})
			]);
			users = usersResponse.users;
			availableRoles = rolesResponse.roles;
		} catch (error: unknown) {
			toast.error('Failed to load users');
			console.error(error);
		} finally {
			loading = false;
		}
	}

	async function fetchRoles() {
		try {
			const resp = await rpcClient.role.listRoles({});
			availableRoles = resp.roles;
		} catch (error: unknown) {
			console.error('Failed to fetch roles:', error);
		}
	}

	async function loadInvites() {
		invitesLoading = true;
		try {
			const resp = await rpcClient.auth.listInvites({});
			invites = resp.invites;
		} catch {
			// User may not have permission
		} finally {
			invitesLoading = false;
		}
	}

	async function createUser() {
		if (!newUserForm.username || !newUserForm.password) {
			toast.error('Username and password are required');
			return;
		}

		if (newUserForm.password.length < 8) {
			toast.error('Password must be at least 8 characters');
			return;
		}

		try {
			const request = create(CreateUserRequestSchema, {
				username: newUserForm.username,
				email: newUserForm.email,
				password: newUserForm.password,
				roles: newUserForm.roles
			});
			await rpcClient.user.createUser(request);

			toast.success('User created successfully');
			showCreateDialog = false;
			newUserForm = {
				username: '',
				email: '',
				password: '',
				roles: []
			};
			await loadUsers();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create user');
		}
	}

	async function updateUser() {
		if (!editingUser) return;

		try {
			const request = create(UpdateUserRequestSchema, {
				id: editingUser.id,
				email: editUserForm.email || undefined,
				roles: editUserForm.roles,
				isActive: editUserForm.isActive
			});
			await rpcClient.user.updateUser(request);

			toast.success('User updated successfully');
			showEditDialog = false;
			editingUser = null;
			await loadUsers();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to update user');
		}
	}

	async function deleteUser(user: User) {
		if (!confirm(`Are you sure you want to delete user "${user.username}"?`)) {
			return;
		}

		try {
			const request = create(DeleteUserRequestSchema, { id: user.id });
			await rpcClient.user.deleteUser(request);

			toast.success('User deleted successfully');
			await loadUsers();
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to delete user');
		}
	}

	async function createInvite() {
		creatingInvite = true;
		try {
			const unitMultipliers = { hours: 1, days: 24, weeks: 168 };
			const expiresInHours = newInviteForm.expiresValue
				? newInviteForm.expiresValue * unitMultipliers[newInviteForm.expiresUnit]
				: undefined;
			const req = create(CreateInviteRequestSchema, {
				description: newInviteForm.description,
				roles: newInviteForm.roles,
				maxUses: newInviteForm.maxUses || 0,
				pin: newInviteForm.pin || undefined,
				expiresInHours,
			});
			const resp = await rpcClient.auth.createInvite(req);
			if (resp.invite) {
				invites = [resp.invite, ...invites];
				const url = `${window.location.origin}/login?invite=${resp.invite.code}`;
				await navigator.clipboard.writeText(url);
				toast.success('Invite created and URL copied to clipboard');
			}
			showCreateInviteDialog = false;
			newInviteForm = { description: '', roles: [], maxUses: null, pin: '', expiresValue: null, expiresUnit: 'hours' };
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to create invite');
		} finally {
			creatingInvite = false;
		}
	}

	async function copyInviteUrl(code: string) {
		const url = `${window.location.origin}/login?invite=${code}`;
		await navigator.clipboard.writeText(url);
		toast.success('Invite URL copied to clipboard');
	}

	async function deleteInvite(id: string) {
		try {
			await rpcClient.auth.deleteInvite(create(DeleteInviteRequestSchema, { id }));
			invites = invites.filter(i => i.id !== id);
			toast.success('Invite revoked');
		} catch (error: unknown) {
			toast.error(error instanceof Error ? error.message : 'Failed to revoke invite');
		}
	}

	function openEditDialog(user: User) {
		fetchRoles();
		editingUser = user;
		editUserForm = {
			email: user.email || '',
			roles: [...(user.roles || [])],
			isActive: user.isActive
		};
		showEditDialog = true;
	}

	function toggleRole(form: { roles: string[] }, roleName: string) {
		const idx = form.roles.indexOf(roleName);
		if (idx >= 0) {
			form.roles = form.roles.filter(r => r !== roleName);
		} else {
			form.roles = [...form.roles, roleName];
		}
	}

	function toggleInviteRole(roleName: string) {
		if (newInviteForm.roles.includes(roleName)) {
			newInviteForm.roles = newInviteForm.roles.filter(r => r !== roleName);
		} else {
			newInviteForm.roles = [...newInviteForm.roles, roleName];
		}
	}

	function formatDate(dateString: string) {
		return new Date(dateString).toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	let inviteRoleNames = $derived(
		availableRoles.filter(r => r.name !== 'anonymous').map(r => r.name)
	);

	onMount(() => {
		loadUsers();
		loadInvites();
	});
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<p class="text-sm text-muted-foreground">Manage user accounts, role assignments, and invite links</p>
		<div class="flex items-center gap-2">
			{#if canCreate}
				<Button variant="outline" onclick={() => { 
					fetchRoles();
					showCreateInviteDialog = true;
				}}>
					<TicketPlus class="mr-2 h-4 w-4" />
					Create Invite
				</Button>
				<Button onclick={() => {
					fetchRoles();
					showCreateDialog = true;
				}}>
					<UserPlus class="mr-2 h-4 w-4" />
					Add User
				</Button>
			{/if}
		</div>
	</div>

	<div class="grid gap-6 xl:grid-cols-[1fr,380px]">
		<!-- User Table -->
		<Card>
			<CardContent>
				{#if loading}
					<div class="flex items-center justify-center py-16">
						<div class="text-center space-y-3">
							<Loader2 class="h-8 w-8 mx-auto animate-spin text-primary" />
							<div class="text-muted-foreground">Loading users...</div>
						</div>
					</div>
				{:else if users.length === 0}
					<div class="flex items-center justify-center py-16">
						<div class="text-center space-y-3">
							<Users class="h-12 w-12 mx-auto text-muted-foreground" />
							<div class="text-muted-foreground">No users found</div>
						</div>
					</div>
				{:else}
					<Table>
						<TableHeader>
							<TableRow>
								<TableHead>Username</TableHead>
								<TableHead>Email</TableHead>
								<TableHead>Provider</TableHead>
								<TableHead>Roles</TableHead>
								<TableHead>Status</TableHead>
								<TableHead>Created</TableHead>
								{#if canUpdate || canDelete}
									<TableHead class="text-right">Actions</TableHead>
								{/if}
							</TableRow>
						</TableHeader>
						<TableBody>
							{#each users as user (user.id)}
								<TableRow>
									<TableCell class="font-medium">{user.username}</TableCell>
									<TableCell>{user.email || '-'}</TableCell>
									<TableCell>
										<Badge variant="outline" class="capitalize">{user.authProvider || 'local'}</Badge>
									</TableCell>
									<TableCell>
										<div class="flex flex-wrap gap-1">
											{#each user.roles || [] as role (role)}
												<Badge variant={getRoleBadgeVariant(role)}>
													{role}
												</Badge>
											{/each}
											{#if !user.roles?.length}
												<span class="text-muted-foreground text-sm">None</span>
											{/if}
										</div>
									</TableCell>
									<TableCell>
										{#if user.isActive}
											<Badge variant="outline" class="text-green-600">Active</Badge>
										{:else}
											<Badge variant="outline" class="text-red-600">Inactive</Badge>
										{/if}
									</TableCell>
									<TableCell class="text-sm text-muted-foreground">
										{user.createdAt ? formatDate(new Date(Number(user.createdAt.seconds) * 1000).toISOString()) : 'Unknown'}
									</TableCell>
									{#if canUpdate || canDelete}
										<TableCell class="text-right">
											<div class="flex justify-end gap-2">
												{#if canUpdate}
													<Button
														size="sm"
														variant="outline"
														onclick={() => openEditDialog(user)}
													>
														<Edit class="h-4 w-4" />
													</Button>
												{/if}
												{#if canDelete}
													<Button
														size="sm"
														variant="outline"
														onclick={() => deleteUser(user)}
														disabled={user.id === $authStore.user?.id}
													>
														<Trash2 class="h-4 w-4" />
													</Button>
												{/if}
											</div>
										</TableCell>
									{/if}
								</TableRow>
							{/each}
						</TableBody>
					</Table>
				{/if}
			</CardContent>
		</Card>

		<!-- Invite Links Card -->
		<Card>
			<CardHeader class="pb-3">
				<div class="flex items-center gap-3">
					<TicketPlus class="h-5 w-5 text-primary" />
					<div>
						<CardTitle class="text-lg">Invite Links</CardTitle>
						<CardDescription>Controlled registration via shareable URLs</CardDescription>
					</div>
				</div>
			</CardHeader>
			<CardContent>
				{#if invitesLoading}
					<div class="flex items-center justify-center py-8">
						<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
					</div>
				{:else if invites.length === 0}
					<div class="rounded-lg border border-dashed p-6 text-center">
						<TicketPlus class="h-8 w-8 text-muted-foreground mx-auto mb-2" />
						<p class="text-sm text-muted-foreground">
							No invite links yet. Create one to allow controlled registration.
						</p>
					</div>
				{:else}
					<div class="space-y-2">
						{#each invites as invite (invite.id)}
							{@const status = getInviteStatus(invite)}
							<div class="p-3 rounded-lg border bg-card">
								<div class="flex items-center justify-between gap-2">
									<div class="flex items-center gap-2 min-w-0">
										<span class="text-sm font-medium truncate">{invite.description || 'Untitled'}</span>
										<Badge variant={getStatusVariant(status)} class="text-[10px] shrink-0">
											{status}
										</Badge>
										{#if invite.hasPin}
											<Badge variant="outline" class="text-[10px] shrink-0">PIN</Badge>
										{/if}
									</div>
									<div class="flex items-center gap-0.5 shrink-0">
										<Button
											variant="ghost"
											size="icon"
											class="h-7 w-7"
											onclick={() => copyInviteUrl(invite.code)}
											title="Copy invite URL"
										>
											<Copy class="h-3.5 w-3.5" />
										</Button>
										{#if canDelete}
											<Button
												variant="ghost"
												size="icon"
												class="h-7 w-7 text-destructive hover:text-destructive"
												onclick={() => deleteInvite(invite.id)}
												title="Revoke invite"
											>
												<Trash2 class="h-3.5 w-3.5" />
											</Button>
										{/if}
									</div>
								</div>
								<div class="flex items-center gap-2 mt-1 text-[11px] text-muted-foreground flex-wrap">
									<span>{invite.useCount}{invite.maxUses > 0 ? `/${invite.maxUses}` : '/\u221e'} uses</span>
									{#if invite.roles && invite.roles.length > 0}
										<span class="text-muted-foreground/50">|</span>
										<span>{invite.roles.join(', ')}</span>
									{/if}
									{#if invite.expiresAt}
										<span class="text-muted-foreground/50">|</span>
										<span>{new Date(Number(invite.expiresAt.seconds) * 1000).toLocaleDateString()}</span>
									{/if}
								</div>
							</div>
						{/each}
					</div>
				{/if}
			</CardContent>
		</Card>
	</div>
</div>

<!-- Create User Dialog -->
<Dialog open={showCreateDialog} onOpenChange={(open) => showCreateDialog = open}>
	<DialogContent class="!max-w-3xl !w-[90vw] !h-[70vh] !p-0 !gap-0 overflow-hidden flex flex-col" showCloseButton={false}>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="w-64 border-r bg-muted/30 flex flex-col">
				<div class="p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
							<UserPlus class="h-6 w-6 text-primary" />
						</div>
						<div class="flex-1 min-w-0">
							<h3 class="font-semibold">New User</h3>
							<p class="text-xs text-muted-foreground mt-0.5">Local account</p>
						</div>
					</div>
				</div>

				<div class="flex-1 p-4 space-y-4">
					<div class="space-y-3">
						<div class="flex items-start gap-3 text-sm">
							<KeyRound class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Creates a local account with password authentication.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<Shield class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Assign roles to control what the user can access.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<Mail class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Email is optional and used for display purposes only.</p>
						</div>
					</div>
				</div>

				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-1">Password policy</p>
						<p class="text-xs text-muted-foreground">
							Passwords must be at least 8 characters. The user can change their password later.
						</p>
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex-1 flex flex-col min-w-0">
				<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">Create User</h2>
						<p class="text-muted-foreground mt-1">Add a new user to the system</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => showCreateDialog = false} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<div class="flex-1 overflow-y-auto p-8">
					<div class="space-y-6">
						<div class="space-y-2">
							<Label for="new-username">Username</Label>
							<Input
								id="new-username"
								type="text"
								bind:value={newUserForm.username}
								placeholder="username"
								required
							/>
						</div>
						<div class="space-y-2">
							<div class="flex items-center gap-2">
								<Mail class="h-4 w-4 text-muted-foreground" />
								<Label for="new-email">Email (optional)</Label>
							</div>
							<Input
								id="new-email"
								type="email"
								bind:value={newUserForm.email}
								placeholder="user@example.com"
							/>
						</div>
						<div class="space-y-2">
							<div class="flex items-center gap-2">
								<KeyRound class="h-4 w-4 text-muted-foreground" />
								<Label for="new-password">Password</Label>
							</div>
							<Input
								id="new-password"
								type="password"
								bind:value={newUserForm.password}
								placeholder="Minimum 8 characters"
								required
							/>
						</div>
						<div class="space-y-2">
							<div class="flex items-center gap-2">
								<Shield class="h-4 w-4 text-muted-foreground" />
								<Label>Roles</Label>
							</div>
							<div class="flex flex-wrap gap-2">
								{#each availableRoles as role (role.id)}
									<Button
										size="sm"
										variant={newUserForm.roles.includes(role.name) ? 'default' : 'outline'}
										onclick={() => toggleRole(newUserForm, role.name)}
									>
										{#if newUserForm.roles.includes(role.name)}
											<Check class="mr-1 h-3 w-3" />
										{/if}
										{role.name}
									</Button>
								{/each}
							</div>
						</div>
					</div>
				</div>

				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					<Button variant="outline" onclick={() => showCreateDialog = false} class="h-11 px-6">
						Cancel
					</Button>
					<Button onclick={createUser} class="h-11 px-8 gap-2">
						<UserPlus class="h-4 w-4" />
						Create User
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>

<!-- Edit User Dialog -->
<Dialog open={showEditDialog} onOpenChange={(open) => showEditDialog = open}>
	<DialogContent class="!max-w-3xl !w-[90vw] !h-[70vh] !p-0 !gap-0 overflow-hidden flex flex-col" showCloseButton={false}>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="w-64 border-r bg-muted/30 flex flex-col">
				<div class="p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
							<UserCog class="h-6 w-6 text-primary" />
						</div>
						<div class="flex-1 min-w-0">
							<h3 class="font-semibold truncate">{editingUser?.username ?? 'User'}</h3>
							<p class="text-xs text-muted-foreground mt-0.5 capitalize">{editingUser?.authProvider || 'local'} account</p>
						</div>
					</div>
				</div>

				<div class="flex-1 p-4 space-y-4">
					{#if editingUser}
						<div class="space-y-3">
							<div class="flex items-start gap-3 text-sm">
								<Shield class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
								<div>
									<p class="text-muted-foreground">Current roles</p>
									<div class="flex flex-wrap gap-1 mt-1">
										{#each editingUser.roles || [] as role (role)}
											<Badge variant={getRoleBadgeVariant(role)} class="text-[10px]">{role}</Badge>
										{/each}
										{#if !editingUser.roles?.length}
											<span class="text-xs text-muted-foreground">None</span>
										{/if}
									</div>
								</div>
							</div>
							<div class="flex items-start gap-3 text-sm">
								<Clock class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
								<div>
									<p class="text-muted-foreground">Created</p>
									<p class="text-xs mt-0.5">
										{editingUser.createdAt ? formatDate(new Date(Number(editingUser.createdAt.seconds) * 1000).toISOString()) : 'Unknown'}
									</p>
								</div>
							</div>
						</div>
					{/if}
				</div>

				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-1">User management</p>
						<p class="text-xs text-muted-foreground">
							Changes to roles and status take effect immediately. The username cannot be changed.
						</p>
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex-1 flex flex-col min-w-0">
				<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">Edit User</h2>
						<p class="text-muted-foreground mt-1">Update account details and permissions</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => showEditDialog = false} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<div class="flex-1 overflow-y-auto p-8">
					{#if editingUser}
						<div class="space-y-6">
							<div class="space-y-2">
								<Label>Username</Label>
								<Input value={editingUser.username} disabled />
							</div>
							<div class="space-y-2">
								<div class="flex items-center gap-2">
									<Mail class="h-4 w-4 text-muted-foreground" />
									<Label for="edit-email">Email</Label>
								</div>
								<Input
									id="edit-email"
									type="email"
									bind:value={editUserForm.email}
									placeholder="user@example.com"
								/>
							</div>
							<div class="space-y-2">
								<div class="flex items-center gap-2">
									<Shield class="h-4 w-4 text-muted-foreground" />
									<Label>Roles</Label>
								</div>
								<div class="flex flex-wrap gap-2">
									{#each availableRoles as role (role.id)}
										<Button
											size="sm"
											variant={editUserForm.roles.includes(role.name) ? 'default' : 'outline'}
											onclick={() => toggleRole(editUserForm, role.name)}
										>
											{#if editUserForm.roles.includes(role.name)}
												<Check class="mr-1 h-3 w-3" />
											{/if}
											{role.name}
										</Button>
									{/each}
								</div>
							</div>
							<div class="flex items-center justify-between rounded-lg border p-4">
								<div class="space-y-0.5">
									<Label for="edit-active">Account active</Label>
									<p class="text-xs text-muted-foreground">Inactive accounts cannot log in</p>
								</div>
								<Switch
									id="edit-active"
									checked={editUserForm.isActive}
									onCheckedChange={(checked) => editUserForm.isActive = checked}
								/>
							</div>
						</div>
					{/if}
				</div>

				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					<Button variant="outline" onclick={() => showEditDialog = false} class="h-11 px-6">
						Cancel
					</Button>
					<Button onclick={updateUser} class="h-11 px-8 gap-2">
						<Save class="h-4 w-4" />
						Save Changes
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>

<!-- Create Invite Dialog -->
<Dialog open={showCreateInviteDialog} onOpenChange={(open) => showCreateInviteDialog = open}>
	<DialogContent class="!max-w-3xl !w-[90vw] !h-[70vh] !p-0 !gap-0 overflow-hidden flex flex-col" showCloseButton={false}>
		<div class="flex h-full">
			<!-- Sidebar -->
			<div class="w-64 border-r bg-muted/30 flex flex-col">
				<!-- Sidebar Header -->
				<div class="p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="h-12 w-12 rounded-xl bg-primary/10 flex items-center justify-center">
							<TicketPlus class="h-6 w-6 text-primary" />
						</div>
						<div class="flex-1 min-w-0">
							<h3 class="font-semibold">New Invite</h3>
							<p class="text-xs text-muted-foreground mt-0.5">Registration link</p>
						</div>
					</div>
				</div>

				<!-- Info -->
				<div class="flex-1 p-4 space-y-4">
					<div class="space-y-3">
						<div class="flex items-start gap-3 text-sm">
							<Link class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Generates a unique URL that allows someone to register an account.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<Shield class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Assigned roles are applied automatically on registration.</p>
						</div>
						<div class="flex items-start gap-3 text-sm">
							<Lock class="h-4 w-4 text-muted-foreground mt-0.5 shrink-0" />
							<p class="text-muted-foreground">Optional PIN adds an extra layer of protection.</p>
						</div>
					</div>
				</div>

				<!-- Sidebar Footer -->
				<div class="p-4 border-t">
					<div class="p-4 rounded-lg bg-muted/50">
						<p class="text-sm font-medium mb-1">How it works</p>
						<p class="text-xs text-muted-foreground">
							After creation, the invite URL is copied to your clipboard. Share it with the person you want to invite.
						</p>
					</div>
				</div>
			</div>

			<!-- Main Content -->
			<div class="flex-1 flex flex-col min-w-0">
				<!-- Content Header -->
				<div class="flex items-center justify-between px-8 py-6 border-b bg-muted/30">
					<div>
						<h2 class="text-2xl font-semibold tracking-tight">Create Invite</h2>
						<p class="text-muted-foreground mt-1">Configure invite settings and restrictions</p>
					</div>
					<Button variant="ghost" size="icon" onclick={() => showCreateInviteDialog = false} class="h-10 w-10">
						<X class="h-5 w-5" />
					</Button>
				</div>

				<!-- Scrollable Content Area -->
				<div class="flex-1 overflow-y-auto p-8">
					<div class="space-y-6">
						<div class="space-y-2">
							<Label for="invite-desc">Description</Label>
							<Input
								id="invite-desc"
								bind:value={newInviteForm.description}
								placeholder="e.g. For server admins"
								disabled={creatingInvite}
							/>
						</div>

						<div class="space-y-2">
							<Label>Roles</Label>
							<p class="text-xs text-muted-foreground">Assigned on registration. If none selected, default roles are used.</p>
							<div class="flex flex-wrap gap-2 mt-1">
								{#each inviteRoleNames as role (role)}
									<Button
										size="sm"
										variant={newInviteForm.roles.includes(role) ? 'default' : 'outline'}
										onclick={() => toggleInviteRole(role)}
										disabled={creatingInvite}
									>
										{#if newInviteForm.roles.includes(role)}
											<Check class="mr-1 h-3 w-3" />
										{/if}
										{role}
									</Button>
								{/each}
							</div>
						</div>

						<div class="grid grid-cols-2 gap-4">
							<div class="space-y-2">
								<div class="flex items-center gap-2">
									<Hash class="h-4 w-4 text-muted-foreground" />
									<Label for="invite-max-uses">Max Uses</Label>
								</div>
								<Input
									id="invite-max-uses"
									type="number"
									min="1"
									bind:value={newInviteForm.maxUses}
									placeholder="Unlimited"
									disabled={creatingInvite}
								/>
							</div>
							<div class="space-y-2">
								<div class="flex items-center gap-2">
									<Clock class="h-4 w-4 text-muted-foreground" />
									<Label>Expiration</Label>
								</div>
								<div class="flex gap-2">
									<Input
										type="number"
										min="1"
										class="flex-1"
										bind:value={newInviteForm.expiresValue}
										placeholder="Never"
										disabled={creatingInvite}
									/>
									<Select
										value={newInviteForm.expiresUnit}
										type="single"
										onValueChange={(v) => { if (v) newInviteForm.expiresUnit = v as 'hours' | 'days' | 'weeks'; }}
										disabled={creatingInvite || !newInviteForm.expiresValue}
									>
										<SelectTrigger class="h-9 w-[100px]">
											<span class="capitalize">{newInviteForm.expiresUnit}</span>
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="hours">Hours</SelectItem>
											<SelectItem value="days">Days</SelectItem>
											<SelectItem value="weeks">Weeks</SelectItem>
										</SelectContent>
									</Select>
								</div>
							</div>
						</div>

						<div class="space-y-2">
							<div class="flex items-center gap-2">
								<Lock class="h-4 w-4 text-muted-foreground" />
								<Label for="invite-pin">PIN Protection</Label>
							</div>
							<Input
								id="invite-pin"
								type="password"
								bind:value={newInviteForm.pin}
								placeholder="Optional"
								disabled={creatingInvite}
							/>
							<p class="text-xs text-muted-foreground">If set, users must enter this PIN to use the invite.</p>
						</div>
					</div>
				</div>

				<!-- Footer -->
				<div class="flex items-center justify-end gap-3 px-8 py-5 border-t bg-muted/30">
					<Button variant="outline" onclick={() => showCreateInviteDialog = false} disabled={creatingInvite} class="h-11 px-6">
						Cancel
					</Button>
					<Button onclick={createInvite} disabled={creatingInvite} class="h-11 px-8 gap-2">
						{#if creatingInvite}
							<Loader2 class="h-4 w-4 animate-spin" />
							Creating...
						{:else}
							<TicketPlus class="h-4 w-4" />
							Create & Copy URL
						{/if}
					</Button>
				</div>
			</div>
		</div>
	</DialogContent>
</Dialog>
