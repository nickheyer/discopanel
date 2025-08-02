import { writable } from 'svelte/store';
import type { Server } from './api';

// Global store for servers list
export const servers = writable<Server[]>([]);

// Loading states
export const loading = writable<boolean>(false);

// Error messages
export const error = writable<string | null>(null);

// Selected server for detailed view
export const selectedServer = writable<Server | null>(null);