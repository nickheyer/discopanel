import { writable, derived, get } from 'svelte/store';
import { goto } from '$app/navigation';
import { browser } from '$app/environment';
import { create } from '@bufbuild/protobuf';
import { rpcClient } from '$lib/api/rpc-client';
import type { User } from '$lib/proto/discopanel/v1/common_pb';
import { UserRole } from '$lib/proto/discopanel/v1/common_pb';
import {
	LoginRequestSchema,
	RegisterRequestSchema,
	ResetPasswordRequestSchema,
	ChangePasswordRequestSchema
} from '$lib/proto/discopanel/v1/auth_pb';

interface AuthState {
	user: User | null;
	token: string | null;
	isAuthenticated: boolean;
	isLoading: boolean;
	authEnabled: boolean;
	firstUserSetup: boolean;
	allowRegistration: boolean;
}

function createAuthStore() {
	const { subscribe, set, update } = writable<AuthState>({
		user: null,
		token: null,
		isAuthenticated: false,
		isLoading: true,
		authEnabled: false,
		firstUserSetup: false,
		allowRegistration: false,
	});

	// Load token from localStorage on init
	if (browser) {
		const token = localStorage.getItem('auth_token');
		if (token) {
			update(state => ({ ...state, token }));
		}
	}

	return {
		subscribe,
		
		async checkAuthStatus() {
			try {
				const response = await rpcClient.auth.getAuthStatus({});

				update(state => ({
					...state,
					authEnabled: response.enabled,
					firstUserSetup: response.firstUserSetup,
					allowRegistration: response.allowRegistration,
				}));

				// If auth is enabled and we have a token, validate it
				let currentToken: string | null = null;
				update(state => {
					currentToken = state.token;
					return state;
				});

				if (response.enabled && currentToken) {
					await this.validateSession();
				} else {
					update(state => ({ ...state, isLoading: false }));
				}

				return {
					enabled: response.enabled,
					firstUserSetup: response.firstUserSetup,
					allowRegistration: response.allowRegistration
				};
			} catch (error) {
				console.error('Failed to check auth status:', error);
				update(state => ({ ...state, isLoading: false }));
				return { enabled: false, firstUserSetup: false, allowRegistration: false };
			}
		},
		
		async login(username: string, password: string) {
			try {
				const request = create(LoginRequestSchema, { username, password });
				const response = await rpcClient.auth.login(request);

				// Store token
				if (browser && response.token) {
					localStorage.setItem('auth_token', response.token);
				}

				update(state => ({
					...state,
					user: response.user || null,
					token: response.token,
					isAuthenticated: true,
					isLoading: false,
				}));

				return response;
			} catch (error) {
				update(state => ({ ...state, isLoading: false }));
				throw error;
			}
		},
		
		async logout() {
			let currentState: AuthState = {
				user: null,
				token: null,
				isAuthenticated: false,
				isLoading: false,
				authEnabled: false,
				firstUserSetup: false,
				allowRegistration: false,
			};

			update(state => {
				currentState = state;
				return state;
			});

			try {
				if (currentState.token) {
					await rpcClient.auth.logout({});
				}
			} catch (error) {
				console.error('Logout error:', error);
			}

			// Clear local storage
			if (browser) {
				localStorage.removeItem('auth_token');
			}

			// Reset state
			set({
				user: null,
				token: null,
				isAuthenticated: false,
				isLoading: false,
				authEnabled: currentState.authEnabled,
				firstUserSetup: currentState.firstUserSetup,
				allowRegistration: currentState.allowRegistration,
			});

			// Redirect to login
			goto('/login');
		},
		
		async register(username: string, email: string, password: string) {
			try {
				const request = create(RegisterRequestSchema, { username, email, password });
				await rpcClient.auth.register(request);

				// After successful registration, log them in
				return await this.login(username, password);
			} catch (error) {
				throw error;
			}
		},
		
		async changePassword(oldPassword: string, newPassword: string) {
			try {
				const request = create(ChangePasswordRequestSchema, {
					oldPassword,
					newPassword
				});
				const response = await rpcClient.auth.changePassword(request);
				return response;
			} catch (error: any) {
				throw new Error(error.message || 'Failed to change password');
			}
		},
		
		async resetPassword(username: string, recoveryKey: string, newPassword: string) {
			try {
				const request = create(ResetPasswordRequestSchema, {
					username,
					recoveryKey,
					newPassword
				});
				const response = await rpcClient.auth.resetPassword(request);
				return response;
			} catch (error: any) {
				throw new Error(error.message || 'Failed to reset password');
			}
		},
		
		async validateSession() {
			let currentToken: string | null = null;
			update(state => {
				currentToken = state.token;
				return state;
			});

			if (!currentToken) {
				update(state => ({ ...state, isLoading: false }));
				return false;
			}

			try {
				const response = await rpcClient.auth.getCurrentUser({});

				if (response.user) {
					update(state => ({
						...state,
						user: response.user || null,
						isAuthenticated: true,
						isLoading: false,
					}));
					return true;
				} else {
					// Invalid token, clear it
					if (browser) {
						localStorage.removeItem('auth_token');
					}
					update(state => ({
						...state,
						user: null,
						token: null,
						isAuthenticated: false,
						isLoading: false,
					}));
					return false;
				}
			} catch (error) {
				console.error('Session validation error:', error);
				// Invalid token, clear it
				if (browser) {
					localStorage.removeItem('auth_token');
				}
				update(state => ({
					...state,
					user: null,
					token: null,
					isAuthenticated: false,
					isLoading: false,
				}));
				return false;
			}
		},
		
		getHeaders() {
			let currentToken: string | null = null;
			update(state => {
				currentToken = state.token;
				return state;
			});
			
			const headers: HeadersInit = {};
			if (currentToken) {
				headers['Authorization'] = `Bearer ${currentToken}`;
			}
			return headers;
		},
	};
}

export const authStore = createAuthStore();

// Derived stores for convenience
export const isAuthenticated = derived(authStore, $auth => $auth.isAuthenticated);
export const currentUser = derived(authStore, $auth => $auth.user);
export const isAdmin = derived(authStore, $auth => $auth.user?.role === UserRole.ADMIN);
export const isEditor = derived(authStore, $auth => $auth.user?.role === UserRole.EDITOR || $auth.user?.role === UserRole.ADMIN);
export const authEnabled = derived(authStore, $auth => $auth.authEnabled);

// Make auth store values accessible as a readable store
export const $authStore = derived(authStore, $auth => $auth);