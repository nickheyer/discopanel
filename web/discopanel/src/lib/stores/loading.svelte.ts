import { writable } from 'svelte/store';

interface LoadingState {
	global: boolean;
	operations: Set<string>;
}

function createLoadingStore() {
	const { subscribe, update } = writable<LoadingState>({
		global: false,
		operations: new Set()
	});

	return {
		subscribe,
		start(operationId?: string) {
			update(state => {
				if (operationId) {
					state.operations.add(operationId);
				}
				state.global = state.operations.size > 0;
				return state;
			});
		},
		stop(operationId?: string) {
			update(state => {
				if (operationId) {
					state.operations.delete(operationId);
				}
				state.global = state.operations.size > 0;
				return state;
			});
		},
		isLoading(operationId?: string): boolean {
			let loading = false;
			subscribe(state => {
				loading = operationId ? state.operations.has(operationId) : state.global;
			})();
			return loading;
		},
		clear() {
			update(state => {
				state.operations.clear();
				state.global = false;
				return state;
			});
		}
	};
}

export const loadingStore = createLoadingStore();