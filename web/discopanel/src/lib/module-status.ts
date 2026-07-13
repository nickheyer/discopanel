import { ModuleStatus } from '$lib/proto/discopanel/v1/module_pb';
import type { StatusTone } from '$lib/server-status';

export interface ModuleStatusMeta {
	label: string;
	tone: StatusTone;
	transitional: boolean;
}

const STATUS_META: Record<ModuleStatus, ModuleStatusMeta> = {
	[ModuleStatus.UNSPECIFIED]: { label: 'Unknown', tone: 'idle', transitional: false },
	[ModuleStatus.STOPPED]: { label: 'Stopped', tone: 'idle', transitional: false },
	[ModuleStatus.STARTING]: { label: 'Starting', tone: 'busy', transitional: true },
	[ModuleStatus.RUNNING]: { label: 'Running', tone: 'ok', transitional: false },
	[ModuleStatus.STOPPING]: { label: 'Stopping', tone: 'busy', transitional: true },
	[ModuleStatus.ERROR]: { label: 'Error', tone: 'danger', transitional: false },
	[ModuleStatus.CREATING]: { label: 'Creating', tone: 'busy', transitional: true }
};

export function moduleStatusMeta(status: ModuleStatus): ModuleStatusMeta {
	return STATUS_META[status] ?? STATUS_META[ModuleStatus.UNSPECIFIED];
}
