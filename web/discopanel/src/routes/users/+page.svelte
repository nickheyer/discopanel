<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { authStore, isAdmin } from '$lib/stores/auth';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '$lib/components/ui/table';
	import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Users, UserPlus, Trash2, Edit, Shield, Eye, AlertCircle, Loader2 } from '@lucide/svelte';
	import { create } from '@bufbuild/protobuf';
	import { rpcClient } from '$lib/api/rpc-client';
	import type { User } from '$lib/proto/discopanel/v1/common_pb';
	import { UserRole } from '$lib/proto/discopanel/v1/common_pb';
	import { CreateUserRequestSchema, UpdateUserRequestSchema, DeleteUserRequestSchema } from '$lib/proto/discopanel/v1/user_pb';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let isUserAdmin = $derived($isAdmin);
	let showCreateDialog = $state(false);
	let showEditDialog = $state(false);
	let editingUser = $state<User | null>(null);

	let newUserForm = $state({
		username: '',
		email: '',
		password: '',
		role: UserRole.VIEWER
	});

	let editUserForm = $state({
		email: '',
		role: UserRole.VIEWER,
		isActive: true
	});
	
	async function loadUsers() {
		loading = true;
		try {
			const response = await rpcClient.user.listUsers({});
			users = response.users;
		} catch (error: any) {
			if (error.code === 'PERMISSION_DENIED') {
				toast.error('Admin access required');
				goto('/');
				return;
			}
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
			const request = create(CreateUserRequestSchema, newUserForm);
			await rpcClient.user.createUser(request);

			toast.success('User created successfully');
			showCreateDialog = false;
			newUserForm = {
				username: '',
				email: '',
				password: '',
				role: UserRole.VIEWER
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
				email: editUserForm.email,
				role: editUserForm.role,
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
			role: user.role,
			isActive: user.isActive
		};
		showEditDialog = true;
	}
	
	function getRoleBadge(role: UserRole) {
		switch (role) {
			case UserRole.ADMIN:
				return { variant: 'destructive' as const, icon: Shield };
			case UserRole.EDITOR:
				return { variant: 'secondary' as const, icon: Edit };
			case UserRole.VIEWER:
				return { variant: 'outline' as const, icon: Eye };
			default:
				return { variant: 'outline' as const, icon: Eye };
		}
	}

	function getRoleDisplayName(role: UserRole): string {
		switch (role) {
			case UserRole.ADMIN:
				return 'Admin';
			case UserRole.EDITOR:
				return 'Editor';
			case UserRole.VIEWER:
				return 'Viewer';
			default:
				return 'Unknown';
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
		if (!isUserAdmin) {
			toast.error('Admin access required');
			goto('/');
			return;
		}
		loadUsers();
	});
</script>

<div class="flex-1 space-y-8 p-8 pt-6">
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-4">
			<div class="h-12 w-12 rounded-lg bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center">
				<Users class="h-6 w-6 text-primary" />
			</div>
			<div>
				<h2 class="text-3xl font-bold tracking-tight">User Management</h2>
				<p class="text-muted-foreground">Manage user accounts and permissions</p>
			</div>
		</div>
		
		<Button onclick={() => showCreateDialog = true}>
			<UserPlus class="mr-2 h-4 w-4" />
			Add User
		</Button>
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
							<TableHead>Role</TableHead>
							<TableHead>Status</TableHead>
							<TableHead>Created</TableHead>
							<TableHead>Last Active</TableHead>
							<TableHead class="text-right">Actions</TableHead>
						</TableRow>
					</TableHeader>
					<TableBody>
						{#each users as user}
							<TableRow>
								<TableCell class="font-medium">{user.username}</TableCell>
								<TableCell>{user.email || '-'}</TableCell>
								<TableCell>
									{@const badge = getRoleBadge(user.role)}
									{@const Icon = badge.icon}
									<Badge variant={badge.variant}>
										<Icon class="mr-1 h-3 w-3" />
										{getRoleDisplayName(user.role)}
									</Badge>
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
								<TableCell class="text-sm text-muted-foreground">
									{user.updatedAt ? formatDate(new Date(Number(user.updatedAt.seconds) * 1000).toISOString()) : 'Never'}
								</TableCell>
								<TableCell class="text-right">
									<div class="flex justify-end gap-2">
										<Button 
											size="sm" 
											variant="outline"
											onclick={() => openEditDialog(user)}
										>
											<Edit class="h-4 w-4" />
										</Button>
										<Button 
											size="sm" 
											variant="outline"
											onclick={() => deleteUser(user)}
											disabled={user.id === $authStore.user?.id}
										>
											<Trash2 class="h-4 w-4" />
										</Button>
									</div>
								</TableCell>
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
				Add a new user to the system with specific permissions.
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
				<Label for="new-role">Role</Label>
				<Select
					type="single"
					value={newUserForm.role.toString()}
					onValueChange={(value: string | undefined) => {
						const roleValue = parseInt(value || '1');
						newUserForm.role = roleValue as UserRole;
					}}
				>
					<SelectTrigger id="new-role">
						<span>{getRoleDisplayName(newUserForm.role)}</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value={UserRole.VIEWER.toString()}>Viewer (Read-only)</SelectItem>
						<SelectItem value={UserRole.EDITOR.toString()}>Editor (Manage servers)</SelectItem>
						<SelectItem value={UserRole.ADMIN.toString()}>Admin (Full access)</SelectItem>
					</SelectContent>
				</Select>
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
				Update user information and permissions.
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
					<Label for="edit-role">Role</Label>
					<Select
						type="single"
						value={editUserForm.role.toString()}
						onValueChange={(value: string | undefined) => {
							const roleValue = parseInt(value || '1');
							editUserForm.role = roleValue as UserRole;
						}}
					>
						<SelectTrigger id="edit-role">
							<span>{getRoleDisplayName(editUserForm.role)}</span>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={UserRole.VIEWER.toString()}>Viewer (Read-only)</SelectItem>
							<SelectItem value={UserRole.EDITOR.toString()}>Editor (Manage servers)</SelectItem>
							<SelectItem value={UserRole.ADMIN.toString()}>Admin (Full access)</SelectItem>
						</SelectContent>
					</Select>
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