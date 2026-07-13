import { ServerStatus } from '$lib/proto/discopanel/v1/common_pb';

export type StatusTone = 'ok' | 'busy' | 'warn' | 'danger' | 'sleep' | 'idle';

export interface StatusMeta {
	label: string;
	tone: StatusTone;
	desc: string;
	transitional: boolean;
}

const STATUS_META: Record<ServerStatus, StatusMeta> = {
	[ServerStatus.UNSPECIFIED]: {
		label: 'Unknown',
		tone: 'idle',
		desc: 'Status unknown',
		transitional: false
	},
	[ServerStatus.CREATING]: {
		label: 'Creating',
		tone: 'busy',
		desc: 'Setting up the server container',
		transitional: true
	},
	[ServerStatus.STARTING]: {
		label: 'Starting',
		tone: 'busy',
		desc: 'Booting up, hang tight',
		transitional: true
	},
	[ServerStatus.RUNNING]: {
		label: 'Running',
		tone: 'ok',
		desc: 'Online and accepting players',
		transitional: false
	},
	[ServerStatus.STOPPING]: {
		label: 'Stopping',
		tone: 'busy',
		desc: 'Saving the world and shutting down',
		transitional: true
	},
	[ServerStatus.STOPPED]: {
		label: 'Stopped',
		tone: 'idle',
		desc: 'Offline, start it to play',
		transitional: false
	},
	[ServerStatus.ERROR]: {
		label: 'Error',
		tone: 'danger',
		desc: 'Something went wrong, check the console',
		transitional: false
	},
	[ServerStatus.UNHEALTHY]: {
		label: 'Unhealthy',
		tone: 'warn',
		desc: 'Up but not responding normally',
		transitional: false
	},
	[ServerStatus.PROVISIONING]: {
		label: 'Provisioning',
		tone: 'busy',
		desc: 'Installing server files and mods',
		transitional: true
	},
	[ServerStatus.PAUSED]: {
		label: 'Sleeping',
		tone: 'sleep',
		desc: 'Paused while idle, joins wake it up',
		transitional: false
	}
};

export function statusMeta(status: ServerStatus): StatusMeta {
	return STATUS_META[status] ?? STATUS_META[ServerStatus.UNSPECIFIED];
}

export const TONE_TEXT: Record<StatusTone, string> = {
	ok: 'text-status-ok',
	busy: 'text-status-busy',
	warn: 'text-status-warn',
	danger: 'text-status-danger',
	sleep: 'text-status-sleep',
	idle: 'text-status-idle'
};

export const TONE_BG: Record<StatusTone, string> = {
	ok: 'bg-status-ok',
	busy: 'bg-status-busy',
	warn: 'bg-status-warn',
	danger: 'bg-status-danger',
	sleep: 'bg-status-sleep',
	idle: 'bg-status-idle'
};

export const TONE_BADGE: Record<StatusTone, string> = {
	ok: 'border-status-ok/25 bg-status-ok/10 text-status-ok',
	busy: 'border-status-busy/25 bg-status-busy/10 text-status-busy',
	warn: 'border-status-warn/25 bg-status-warn/10 text-status-warn',
	danger: 'border-status-danger/25 bg-status-danger/10 text-status-danger',
	sleep: 'border-status-sleep/25 bg-status-sleep/10 text-status-sleep',
	idle: 'border-status-idle/25 bg-status-idle/10 text-status-idle'
};

export function canStart(status: ServerStatus): boolean {
	return status === ServerStatus.STOPPED || status === ServerStatus.ERROR;
}

export function canStop(status: ServerStatus): boolean {
	return (
		status === ServerStatus.RUNNING ||
		status === ServerStatus.UNHEALTHY ||
		status === ServerStatus.STARTING ||
		status === ServerStatus.PAUSED ||
		status === ServerStatus.PROVISIONING ||
		status === ServerStatus.ERROR
	);
}

export function canRestart(status: ServerStatus): boolean {
	return (
		status === ServerStatus.RUNNING ||
		status === ServerStatus.UNHEALTHY ||
		status === ServerStatus.ERROR
	);
}

export function isUp(status: ServerStatus): boolean {
	return status === ServerStatus.RUNNING || status === ServerStatus.UNHEALTHY;
}

export function tpsTone(tps: number | undefined): StatusTone {
	if (!tps) return 'idle';
	if (tps >= 18) return 'ok';
	if (tps >= 15) return 'busy';
	return 'danger';
}
