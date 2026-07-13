import { rpcClient } from '$lib/api/rpc-client';
import { serversStore } from '$lib/stores/servers';
import { toast } from 'svelte-sonner';

export type ServerAction = 'start' | 'stop' | 'restart' | 'recreate';

const ACTION_VERBS: Record<ServerAction, string> = {
	start: 'Starting',
	stop: 'Stopping',
	restart: 'Restarting',
	recreate: 'Recreating'
};

export async function runServerAction(
	action: ServerAction,
	server: { id: string; name: string },
	refetch: () => Promise<unknown> = () => serversStore.fetchServers(true)
): Promise<void> {
	try {
		switch (action) {
			case 'start':
				await rpcClient.server.startServer({ id: server.id });
				break;
			case 'stop':
				await rpcClient.server.stopServer({ id: server.id });
				break;
			case 'restart':
				await rpcClient.server.restartServer({ id: server.id });
				break;
			case 'recreate':
				await rpcClient.server.recreateServer({ id: server.id });
				break;
		}
		toast.success(`${ACTION_VERBS[action]} ${server.name}...`);
		await refetch();
	} catch {
		// Interceptor already toasts the failure
	}
}
