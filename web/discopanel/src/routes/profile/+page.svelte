<script lang="ts">
	import { authStore, currentUser } from '$lib/stores/auth';
	import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import { User, Key, Shield, Edit, Eye, Loader2 } from '@lucide/svelte';
	
	let user = $derived($currentUser);
	let passwordForm = $state({
		oldPassword: '',
		newPassword: '',
		confirmPassword: ''
	});
	let saving = $state(false);
	
	async function changePassword() {
		if (!passwordForm.oldPassword || !passwordForm.newPassword) {
			toast.error('Please fill in all fields');
			return;
		}
		
		if (passwordForm.newPassword !== passwordForm.confirmPassword) {
			toast.error('New passwords do not match');
			return;
		}
		
		if (passwordForm.newPassword.length < 8) {
			toast.error('New password must be at least 8 characters');
			return;
		}
		
		saving = true;
		try {
			await authStore.changePassword(passwordForm.oldPassword, passwordForm.newPassword);
			toast.success('Password changed successfully');
			passwordForm = {
				oldPassword: '',
				newPassword: '',
				confirmPassword: ''
			};
		} catch (error: any) {
			toast.error(error.message || 'Failed to change password');
		} finally {
			saving = false;
		}
	}
	
	function getRoleIcon(role: string) {
		switch (role) {
			case 'admin':
				return Shield;
			case 'editor':
				return Edit;
			default:
				return Eye;
		}
	}
	
	function getRoleBadgeVariant(role: string) {
		switch (role) {
			case 'admin':
				return 'destructive' as const;
			case 'editor':
				return 'secondary' as const;
			default:
				return 'outline' as const;
		}
	}
</script>

<div class="flex-1 space-y-8 p-8 pt-6">
	<div class="flex items-center gap-4">
		<div class="h-12 w-12 rounded-lg bg-gradient-to-br from-primary/20 to-primary/10 flex items-center justify-center">
			<User class="h-6 w-6 text-primary" />
		</div>
		<div>
			<h2 class="text-3xl font-bold tracking-tight">Profile</h2>
			<p class="text-muted-foreground">Manage your account settings</p>
		</div>
	</div>
	
	{#if user}
		<div class="grid gap-6 md:grid-cols-2">
			<!-- User Information -->
			<Card>
				<CardHeader>
					<CardTitle class="flex items-center gap-2">
						<User class="h-5 w-5" />
						Account Information
					</CardTitle>
					<CardDescription>Your account details and role</CardDescription>
				</CardHeader>
				<CardContent class="space-y-4">
					<div>
						<Label class="text-muted-foreground">Username</Label>
						<p class="text-lg font-medium">{user.username}</p>
					</div>
					
					{#if user.email}
						<div>
							<Label class="text-muted-foreground">Email</Label>
							<p class="text-lg">{user.email}</p>
						</div>
					{/if}
					
					<div>
						<Label class="text-muted-foreground">Role</Label>
						<div class="mt-1">
							<Badge variant={getRoleBadgeVariant(user.role)}>
								{@const Icon = getRoleIcon(user.role)}
								<Icon class="mr-1 h-3 w-3" />
								{user.role}
							</Badge>
						</div>
					</div>
					
					<div>
						<Label class="text-muted-foreground">Account Created</Label>
						<p>{new Date(user.created_at).toLocaleDateString()}</p>
					</div>
					
					{#if user.last_login}
						<div>
							<Label class="text-muted-foreground">Last Login</Label>
							<p>{new Date(user.last_login).toLocaleString()}</p>
						</div>
					{/if}
				</CardContent>
			</Card>
			
			<!-- Change Password -->
			<Card>
				<CardHeader>
					<CardTitle class="flex items-center gap-2">
						<Key class="h-5 w-5" />
						Change Password
					</CardTitle>
					<CardDescription>Update your account password</CardDescription>
				</CardHeader>
				<CardContent>
					<form onsubmit={(e) => { e.preventDefault(); changePassword(); }} class="space-y-4">
						<div class="space-y-2">
							<Label for="old-password">Current Password</Label>
							<Input
								id="old-password"
								type="password"
								bind:value={passwordForm.oldPassword}
								required
								disabled={saving}
							/>
						</div>
						
						<div class="space-y-2">
							<Label for="new-password">New Password</Label>
							<Input
								id="new-password"
								type="password"
								bind:value={passwordForm.newPassword}
								required
								disabled={saving}
								placeholder="Minimum 8 characters"
							/>
						</div>
						
						<div class="space-y-2">
							<Label for="confirm-password">Confirm New Password</Label>
							<Input
								id="confirm-password"
								type="password"
								bind:value={passwordForm.confirmPassword}
								required
								disabled={saving}
							/>
						</div>
						
						<Button type="submit" disabled={saving} class="w-full">
							{#if saving}
								<Loader2 class="mr-2 h-4 w-4 animate-spin" />
								Changing Password...
							{:else}
								Change Password
							{/if}
						</Button>
					</form>
				</CardContent>
			</Card>
		</div>
	{/if}
</div>