import { writable, derived, get } from 'svelte/store';
import { goto } from '$app/navigation';
import { browser } from '$app/environment';

interface User {
	id: string;
	username: string;
	email: string;
	role: 'admin' | 'editor' | 'viewer';
	is_active: boolean;
	created_at: string;
	last_login?: string;
}

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

	const store = {
		subscribe,

		async checkAuthStatus() {
			try {
				const response = await fetch('/api/v1/auth/status', {
					credentials: 'include', // Ensure cookies are sent
				});
				const data = await response.json();

				update(state => ({
					...state,
					authEnabled: data.enabled,
					firstUserSetup: data.first_user_setup,
					allowRegistration: data.allow_registration,
				}));

				// If auth is enabled, validate session using cookie
				if (data.enabled) {
					await store.validateSession();
				} else {
					update(state => ({ ...state, isLoading: false }));
				}

				return {
					enabled: data.enabled,
					firstUserSetup: data.first_user_setup,
					allowRegistration: data.allow_registration,
					oidcEnabled: data.oidc_enabled || false
				};
			} catch (error) {
				console.error('Failed to check auth status:', error);
				update(state => ({ ...state, isLoading: false }));
				return { enabled: false, firstUserSetup: false, allowRegistration: false, oidcEnabled: false };
			}
		},

		async login(username: string, password: string) {
			try {
				const response = await fetch('/api/v1/auth/login', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json',
					},
					credentials: 'include', // Ensure cookies are sent and received
					body: JSON.stringify({ username, password }),
				});

				if (!response.ok) {
					const error = await response.json();
					throw new Error(error.error || 'Login failed');
				}

				const data = await response.json();

				// Token is stored in HttpOnly cookie by backend
				// Keep token in memory for Authorization headers
				update(state => ({
					...state,
					user: data.user,
					token: data.token,
					isAuthenticated: true,
					isLoading: false,
				}));

				return data;
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
				// Cookie will be sent automatically, but also send Authorization header if we have token in memory
				await fetch('/api/v1/auth/logout', {
					method: 'POST',
					credentials: 'include', // Ensure cookies are sent
					headers: currentState.token ? {
						'Authorization': `Bearer ${currentState.token}`,
					} : {},
				});
			} catch (error) {
				console.error('Logout error:', error);
			}

			// Cookie is cleared by backend
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
				const response = await fetch('/api/v1/auth/register', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json',
					},
					credentials: 'include', // Ensure cookies are sent and received
					body: JSON.stringify({ username, email, password }),
				});

				if (!response.ok) {
					const error = await response.json();
					throw new Error(error.error || 'Registration failed');
				}

				const data = await response.json();

				// After successful registration, log them in
				return await store.login(username, password);
			} catch (error) {
				throw error;
			}
		},

		async changePassword(oldPassword: string, newPassword: string) {
			let currentToken: string | null = null;
			update(state => {
				currentToken = state.token;
				return state;
			});

			if (!currentToken) {
				throw new Error('Not authenticated');
			}

			const response = await fetch('/api/v1/auth/change-password', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json',
					'Authorization': `Bearer ${currentToken}`,
				},
				credentials: 'include', // Ensure cookies are sent
				body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
			});

			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to change password');
			}

			return await response.json();
		},

		async resetPassword(username: string, recoveryKey: string, newPassword: string) {
			const response = await fetch('/api/v1/auth/reset-password', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json',
				},
				credentials: 'include', // Ensure cookies are sent
				body: JSON.stringify({
					username,
					recovery_key: recoveryKey,
					new_password: newPassword
				}),
			});

			if (!response.ok) {
				const error = await response.json();
				throw new Error(error.error || 'Failed to reset password');
			}

			return await response.json();
		},

		async verifyOIDCPassword(password: string) {
			try {
				const response = await fetch('/api/v1/auth/oidc/verify-password', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json',
					},
					credentials: 'include', // Ensure cookies are sent
					body: JSON.stringify({ password }),
				});

				if (!response.ok) {
					const error = await response.json();
					throw new Error(error.error || 'Password verification failed');
				}

				const data = await response.json();

				// Update auth state after successful verification
				update(state => ({
					...state,
					user: data.user,
					token: data.token,
					isAuthenticated: true,
					isLoading: false,
				}));

				return data;
			} catch (error) {
				update(state => ({ ...state, isLoading: false }));
				throw error;
			}
		},

		async validateSession() {
			try {
				// Cookie is sent automatically with the request
				// Backend will extract token from cookie or Authorization header
				const response = await fetch('/api/v1/auth/me', {
					credentials: 'include', // Ensure cookies are sent
				});

				if (response.ok) {
					const user = await response.json();
					update(state => ({
						...state,
						user,
						isAuthenticated: true,
						isLoading: false,
					}));
					return true;
				} else {
					// Invalid session, clear state
					// Cookie will be cleared by backend if needed
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
				update(state => ({ ...state, isLoading: false }));
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

	// On init, validate session using cookie (if auth is enabled)
	// Token will be loaded from cookie automatically by the backend
	if (browser) {
		// Check auth status and validate session if cookie exists
		store.checkAuthStatus().catch(() => {
			// Ignore errors on init
		});
	}

	return store;
}

export const authStore = createAuthStore();

// Derived stores for convenience
export const isAuthenticated = derived(authStore, $auth => $auth.isAuthenticated);
export const currentUser = derived(authStore, $auth => $auth.user);
export const isAdmin = derived(authStore, $auth => $auth.user?.role === 'admin');
export const isEditor = derived(authStore, $auth => $auth.user?.role === 'editor' || $auth.user?.role === 'admin');
export const authEnabled = derived(authStore, $auth => $auth.authEnabled);

// Make auth store values accessible as a readable store
export const $authStore = derived(authStore, $auth => $auth);