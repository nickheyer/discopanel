import { TriggeredEventType } from '$lib/proto/discopanel/v1/storage_pb';
import { TriggeredEventTypeLabel, TriggeredEventTypeDesc } from '$lib/proto/enums.gen';

export interface EventTypeMeta {
	type: TriggeredEventType;
	label: string;
	description: string;
}

// Display order of user selectable event types
const EVENT_TYPE_ORDER: TriggeredEventType[] = [
	TriggeredEventType.SERVER_START,
	TriggeredEventType.SERVER_STOP,
	TriggeredEventType.SERVER_RESTART,
	TriggeredEventType.SERVER_HEALTHY,
	TriggeredEventType.PLAYER_JOIN,
	TriggeredEventType.PLAYER_LEAVE,
	TriggeredEventType.PLAYER_DEATH,
	TriggeredEventType.PLAYER_ADVANCEMENT,
	TriggeredEventType.PLAYER_CHAT
];

// Ordered catalog of user selectable server event types
export const SERVER_EVENT_TYPES: EventTypeMeta[] = EVENT_TYPE_ORDER.map((type) => ({
	type,
	label: TriggeredEventTypeLabel[type],
	description: TriggeredEventTypeDesc[type] ?? ''
}));

// Resolves display label for an event type
export function getEventTypeLabel(type: TriggeredEventType): string {
	return TriggeredEventTypeLabel[type] ?? TriggeredEventTypeLabel[TriggeredEventType.UNSPECIFIED];
}
