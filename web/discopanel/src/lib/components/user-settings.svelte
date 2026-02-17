<script lang="ts">
	import { onMount } from 'svelte';
	import { authStore, canCreateUsers, canUpdateUsers, canDeleteUsers } from '$lib/stores/auth';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Users, UserPlus, Trash2, Edit, Loader2 } from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { User, Role } from '$lib/proto/discopanel/v1/common_pb';
	import { CreateUserRequestSchema, UpdateUserRequestSchema, DeleteUserRequestSchema } from '$lib/proto/discopanel/v1/user_pb';
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

	async function loadUsers() {
		loading = true;
		try {
			const [usersResponse, rolesResponse] = await Promise.all([
				rpcClient.user.listUsers({}),
				rpcClient.role.listRoles({})
			]);
			users = usersResponse.users;
			availableRoles = rolesResponse.roles;
		} catch (error: any) {
			toast.error('Failed to load users');
			console.error(error);
		} finally {
			loading = false;
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
		} catch (error: any) {
			toast.error(error.message || 'Failed to create user');
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
		} catch (error: any) {
			toast.error(error.message || 'Failed to update user');
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
		} catch (error: any) {
			toast.error(error.message || 'Failed to delete user');
		}
	}

	function openEditDialog(user: User) {
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

	function formatDate(dateString: string) {
		return new Date(dateString).toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	onMount(() => {
		loadUsers();
	});
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<p class="text-sm text-muted-foreground">Manage user accounts and role assignments</p>
		{#if canCreate}
			<Button onclick={() => showCreateDialog = true}>
				<UserPlus class="mr-2 h-4 w-4" />
				Add User
			</Button>
		{/if}
	</div>

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
						{#each users as user}
							<TableRow>
								<TableCell class="font-medium">{user.username}</TableCell>
								<TableCell>{user.email || '-'}</TableCell>
								<TableCell>
									<Badge variant="outline" class="capitalize">{user.authProvider || 'local'}</Badge>
								</TableCell>
								<TableCell>
									<div class="flex flex-wrap gap-1">
										{#each user.roles || [] as role}
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
</div>

<!-- Create User Dialog -->
<Dialog open={showCreateDialog} onOpenChange={(open) => showCreateDialog = open}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New User</DialogTitle>
			<DialogDescription>
				Add a new user to the system with specific roles.
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-4">
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
				<Label for="new-email">Email (optional)</Label>
				<Input
					id="new-email"
					type="email"
					bind:value={newUserForm.email}
					placeholder="user@example.com"
				/>
			</div>
			<div class="space-y-2">
				<Label for="new-password">Password</Label>
				<Input
					id="new-password"
					type="password"
					bind:value={newUserForm.password}
					placeholder="Minimum 8 characters"
					required
				/>
			</div>
			<div class="space-y-2">
				<Label>Roles</Label>
				<div class="flex flex-wrap gap-2">
					{#each availableRoles as role}
						<Button
							size="sm"
							variant={newUserForm.roles.includes(role.name) ? 'default' : 'outline'}
							onclick={() => toggleRole(newUserForm, role.name)}
						>
							{role.name}
						</Button>
					{/each}
				</div>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => showCreateDialog = false}>
				Cancel
			</Button>
			<Button onclick={createUser}>
				<UserPlus class="mr-2 h-4 w-4" />
				Create User
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<!-- Edit User Dialog -->
<Dialog open={showEditDialog} onOpenChange={(open) => showEditDialog = open}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Edit User</DialogTitle>
			<DialogDescription>
				Update user information and role assignments.
			</DialogDescription>
		</DialogHeader>

		{#if editingUser}
			<div class="space-y-4">
				<div class="space-y-2">
					<Label>Username</Label>
					<Input value={editingUser.username} disabled />
				</div>
				<div class="space-y-2">
					<Label for="edit-email">Email</Label>
					<Input
						id="edit-email"
						type="email"
						bind:value={editUserForm.email}
						placeholder="user@example.com"
					/>
				</div>
				<div class="space-y-2">
					<Label>Roles</Label>
					<div class="flex flex-wrap gap-2">
						{#each availableRoles as role}
							<Button
								size="sm"
								variant={editUserForm.roles.includes(role.name) ? 'default' : 'outline'}
								onclick={() => toggleRole(editUserForm, role.name)}
							>
								{role.name}
							</Button>
						{/each}
					</div>
				</div>
				<div class="flex items-center space-x-2">
					<input
						type="checkbox"
						id="edit-active"
						bind:checked={editUserForm.isActive}
						class="h-4 w-4"
					/>
					<Label for="edit-active">Account is active</Label>
				</div>
			</div>
		{/if}

		<DialogFooter>
			<Button variant="outline" onclick={() => showEditDialog = false}>
				Cancel
			</Button>
			<Button onclick={updateUser}>
				Save Changes
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
