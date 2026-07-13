import { ServerStatus, ModLoader } from '$lib/proto/discopanel/v1/common_pb';

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
	[ServerStatus.RESTARTING]: {
		label: 'Restarting',
		tone: 'busy',
		desc: 'Back in a moment',
		transitional: true
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

const LOADER_LABELS: Partial<Record<ModLoader, string>> = {
	[ModLoader.VANILLA]: 'Vanilla',
	[ModLoader.FORGE]: 'Forge',
	[ModLoader.FABRIC]: 'Fabric',
	[ModLoader.QUILT]: 'Quilt',
	[ModLoader.PAPER]: 'Paper',
	[ModLoader.SPIGOT]: 'Spigot',
	[ModLoader.BUKKIT]: 'Bukkit',
	[ModLoader.PURPUR]: 'Purpur',
	[ModLoader.SPONGE_VANILLA]: 'Sponge',
	[ModLoader.SPONGE_FORGE]: 'SpongeForge',
	[ModLoader.MOHIST]: 'Mohist',
	[ModLoader.CATSERVER]: 'CatServer',
	[ModLoader.ARCLIGHT]: 'Arclight',
	[ModLoader.AUTO_CURSEFORGE]: 'CurseForge',
	[ModLoader.CURSEFORGE]: 'CurseForge (Auto)',
	[ModLoader.MODRINTH]: 'Modrinth',
	[ModLoader.NEOFORGE]: 'NeoForge',
	[ModLoader.FOLIA]: 'Folia',
	[ModLoader.CUSTOM]: 'Custom',
	[ModLoader.PUFFERFISH]: 'Pufferfish',
	[ModLoader.MAGMA]: 'Magma',
	[ModLoader.MAGMA_MAINTAINED]: 'Magma Maintained',
	[ModLoader.KETTING]: 'Ketting',
	[ModLoader.YOUER]: 'Youer',
	[ModLoader.BANNER]: 'Banner',
	[ModLoader.LIMBO]: 'Limbo',
	[ModLoader.NANO_LIMBO]: 'NanoLimbo',
	[ModLoader.CRUCIBLE]: 'Crucible',
	[ModLoader.GLOWSTONE]: 'Glowstone',
	[ModLoader.FTBA]: 'Feed The Beast'
};

export function loaderLabel(loader: ModLoader): string {
	if (loader === ModLoader.UNSPECIFIED) return '';
	return LOADER_LABELS[loader] ?? ModLoader[loader]?.replace(/_/g, ' ').toLowerCase() ?? '';
}

export function tpsTone(tps: number | undefined): StatusTone {
	if (!tps) return 'idle';
	if (tps >= 18) return 'ok';
	if (tps >= 15) return 'busy';
	return 'danger';
}
