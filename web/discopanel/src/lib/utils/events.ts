import { TriggeredEventType } from '$lib/proto/discopanel/v1/event_pb';

export interface EventTypeMeta {
	type: TriggeredEventType;
	label: string;
	description: string;
}

// Canonical, ordered catalog of user-selectable server event types
export const SERVER_EVENT_TYPES: EventTypeMeta[] = [
	{
		type: TriggeredEventType.SERVER_START,
		label: 'Server Start',
		description: 'When the server starts'
	},
	{
		type: TriggeredEventType.SERVER_STOP,
		label: 'Server Stop',
		description: 'When the server stops'
	},
	{
		type: TriggeredEventType.SERVER_RESTART,
		label: 'Server Restart',
		description: 'When the server restarts'
	},
	{
		type: TriggeredEventType.SERVER_HEALTHY,
		label: 'Server Healthy',
		description: 'When the server passes its health check'
	},
	{
		type: TriggeredEventType.PLAYER_JOIN,
		label: 'Player Join',
		description: 'When a player joins (the player name is available as {{.player}})'
	},
	{
		type: TriggeredEventType.PLAYER_LEAVE,
		label: 'Player Leave',
		description: 'When a player leaves (the player name is available as {{.player}})'
	}
];

// Resolves the display label for an event type else "Unknown"
export function getEventTypeLabel(type: TriggeredEventType): string {
	return SERVER_EVENT_TYPES.find((e) => e.type === type)?.label ?? 'Unknown';
}
