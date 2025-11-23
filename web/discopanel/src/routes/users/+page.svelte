<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { authEnabled, authStore, isAdmin } from '$lib/stores/auth';
	import { Card, CardContent } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Select, SelectContent, SelectItem, SelectTrigger } from '$lib/components/ui/select';
	import {
		Table,
		TableBody,
		TableCell,
		TableHead,
		TableHeader,
		TableRow
	} from '$lib/components/ui/table';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { toast } from 'svelte-sonner';
	import { Users, UserPlus, Trash2, Edit, Shield, Eye, AlertCircle, Loader2 } from '@lucide/svelte';

	interface User {
		id: string;
		username: string;
		email: string;
		role: 'admin' | 'editor' | 'viewer';
		is_active: boolean;
		created_at: string;
		last_login?: string;
	}

	let users = $state<User[]>([]);
	let loading = $state(true);
	let isUserAdmin = $derived($isAdmin);
	let showCreateDialog = $state(false);
	let showEditDialog = $state(false);
	let editingUser = $state<User | null>(null);
	let isAuthEnabled = $derived($authStore.authEnabled);

	let newUserForm = $state({
		username: '',
		email: '',
		password: '',
		role: 'viewer' as 'admin' | 'editor' | 'viewer'
	});

	let editUserForm = $state({
		email: '',
		role: 'viewer' as 'admin' | 'editor' | 'viewer',
		is_active: true
	});

	async function loadUsers() {
		loading = true;
		try {
			const headers = authStore.getHeaders();
			const response = await fetch('/api/v1/users', { headers });

			if (!response.ok) {
				if (response.status === 403) {
					toast.error('Admin access required');
					goto('/');
					return;
				}
				throw new Error('Failed to load users');
			}

			users = await response.json();
		} catch (error) {
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
			const headers = {
				...authStore.getHeaders(),
				'Content-Type': 'application/json'
			};

			const response = await fetch('/api/v1/users', {
				method: 'POST',
				headers,
				body: JSON.stringify(newUserForm)
			});

			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to create user');
			}

			toast.success('User created successfully');
			showCreateDialog = false;
			newUserForm = {
				username: '',
				email: '',
				password: '',
				role: 'viewer'
			};
			await loadUsers();
		} catch (error: any) {
			toast.error(error.message || 'Failed to create user');
		}
	}

	async function updateUser() {
		if (!editingUser) return;

		try {
			const headers = {
				...authStore.getHeaders(),
				'Content-Type': 'application/json'
			};

			const response = await fetch(`/api/v1/users/${editingUser.id}`, {
				method: 'PUT',
				headers,
				body: JSON.stringify(editUserForm)
			});

			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to update user');
			}

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
			const headers = authStore.getHeaders();
			const response = await fetch(`/api/v1/users/${user.id}`, {
				method: 'DELETE',
				headers
			});

			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to delete user');
			}

			toast.success('User deleted successfully');
			await loadUsers();
		} catch (error: any) {
			toast.error(error.message || 'Failed to delete user');
		}
	}

	function openEditDialog(user: User) {
		editingUser = user;
		editUserForm = {
			email: user.email,
			role: user.role,
			is_active: user.is_active
		};
		showEditDialog = true;
	}

	function getRoleBadge(role: string) {
		switch (role) {
			case 'admin':
				return { variant: 'destructive' as const, icon: Shield };
			case 'editor':
				return { variant: 'secondary' as const, icon: Edit };
			default:
				return { variant: 'outline' as const, icon: Eye };
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
		if (!isAuthEnabled) {
			toast.error('Authorization is not activated!');
			goto('/');
			return;
		}

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
			<div
				class="from-primary/20 to-primary/10 flex h-12 w-12 items-center justify-center rounded-lg bg-gradient-to-br"
			>
				<Users class="text-primary h-6 w-6" />
			</div>
			<div>
				<h2 class="text-3xl font-bold tracking-tight">User Management</h2>
				<p class="text-muted-foreground">Manage user accounts and permissions</p>
			</div>
		</div>

		<Button onclick={() => (showCreateDialog = true)}>
			<UserPlus class="mr-2 h-4 w-4" />
			Add User
		</Button>
	</div>

	<Card>
		<CardContent>
			{#if loading}
				<div class="flex items-center justify-center py-16">
					<div class="space-y-3 text-center">
						<Loader2 class="text-primary mx-auto h-8 w-8 animate-spin" />
						<div class="text-muted-foreground">Loading users...</div>
					</div>
				</div>
			{:else if users.length === 0}
				<div class="flex items-center justify-center py-16">
					<div class="space-y-3 text-center">
						<Users class="text-muted-foreground mx-auto h-12 w-12" />
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
							<TableHead>Last Login</TableHead>
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
										{user.role}
									</Badge>
								</TableCell>
								<TableCell>
									{#if user.is_active}
										<Badge variant="outline" class="text-green-600">Active</Badge>
									{:else}
										<Badge variant="outline" class="text-red-600">Inactive</Badge>
									{/if}
								</TableCell>
								<TableCell class="text-muted-foreground text-sm">
									{formatDate(user.created_at)}
								</TableCell>
								<TableCell class="text-muted-foreground text-sm">
									{user.last_login ? formatDate(user.last_login) : 'Never'}
								</TableCell>
								<TableCell class="text-right">
									<div class="flex justify-end gap-2">
										<Button size="sm" variant="outline" onclick={() => openEditDialog(user)}>
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
<Dialog open={showCreateDialog} onOpenChange={(open) => (showCreateDialog = open)}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Create New User</DialogTitle>
			<DialogDescription>Add a new user to the system with specific permissions.</DialogDescription>
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
					value={newUserForm.role}
					onValueChange={(value: string | undefined) =>
						(newUserForm.role = (value || 'viewer') as 'admin' | 'editor' | 'viewer')}
				>
					<SelectTrigger id="new-role">
						<span>{newUserForm.role || 'Select a role'}</span>
					</SelectTrigger>
					<SelectContent>
						<SelectItem value="viewer">Viewer (Read-only)</SelectItem>
						<SelectItem value="editor">Editor (Manage servers)</SelectItem>
						<SelectItem value="admin">Admin (Full access)</SelectItem>
					</SelectContent>
				</Select>
			</div>
		</div>

		<DialogFooter>
			<Button variant="outline" onclick={() => (showCreateDialog = false)}>Cancel</Button>
			<Button onclick={createUser}>
				<UserPlus class="mr-2 h-4 w-4" />
				Create User
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<!-- Edit User Dialog -->
<Dialog open={showEditDialog} onOpenChange={(open) => (showEditDialog = open)}>
	<DialogContent>
		<DialogHeader>
			<DialogTitle>Edit User</DialogTitle>
			<DialogDescription>Update user information and permissions.</DialogDescription>
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
						value={editUserForm.role}
						onValueChange={(value: string | undefined) =>
							(editUserForm.role = (value || 'viewer') as 'admin' | 'editor' | 'viewer')}
					>
						<SelectTrigger id="edit-role">
							<span>{editUserForm.role || 'Select a role'}</span>
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="viewer">Viewer (Read-only)</SelectItem>
							<SelectItem value="editor">Editor (Manage servers)</SelectItem>
							<SelectItem value="admin">Admin (Full access)</SelectItem>
						</SelectContent>
					</Select>
				</div>
				<div class="flex items-center space-x-2">
					<input
						type="checkbox"
						id="edit-active"
						bind:checked={editUserForm.is_active}
						class="h-4 w-4"
					/>
					<Label for="edit-active">Account is active</Label>
				</div>
			</div>
		{/if}

		<DialogFooter>
			<Button variant="outline" onclick={() => (showEditDialog = false)}>Cancel</Button>
			<Button onclick={updateUser}>Save Changes</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
