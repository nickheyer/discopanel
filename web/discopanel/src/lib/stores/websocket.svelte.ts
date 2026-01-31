import { browser } from '$app/environment';
import { create, toBinary, fromBinary } from '@bufbuild/protobuf';
import type { LogEntry } from '$lib/proto/discopanel/v1/server_pb';
import {
	WSMessageType,
	WebSocketClientMessageSchema,
	WebSocketServerMessageSchema,
	AuthMessageSchema,
	SubscribeMessageSchema,
	UnsubscribeMessageSchema,
	CommandMessageSchema,
	type WebSocketServerMessage,
	type LogsMessage,
	type LogMessage,
	type CommandResultMessage
} from '$lib/proto/discopanel/v1/websocket_pb';

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'authenticated';

export interface WebSocketState {
	connectionState: ConnectionState;
	error: string | null;
}

type MessageHandler = (message: WebSocketServerMessage) => void;
type LogHandler = (serverId: string, logs: LogEntry[]) => void;
type LogEntryHandler = (serverId: string, log: LogEntry) => void;
type CommandResultHandler = (result: CommandResultMessage) => void;

class WebSocketClient {
	private socket: WebSocket | null = null;
	private reconnectAttempts = 0;
	private maxReconnectAttempts = 5;
	private reconnectDelay = 1000;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	private pingTimer: ReturnType<typeof setInterval> | null = null;
	private pendingAuth: string | null = null;

	// Svelte 5 runes for reactive state
	state = $state<WebSocketState>({
		connectionState: 'disconnected',
		error: null
	});

	// Event handlers
	private messageHandlers = new Set<MessageHandler>();
	private logHandlers = new Set<LogHandler>();
	private logEntryHandlers = new Set<LogEntryHandler>();
	private commandResultHandlers = new Set<CommandResultHandler>();

	// Active subscriptions (serverId -> true)
	private subscriptions = new Map<string, boolean>();

	connect(): void {
		if (!browser) return;
		if (this.socket?.readyState === WebSocket.OPEN) return;
		if (this.state.connectionState === 'connecting') return;

		this.state.connectionState = 'connecting';
		this.state.error = null;

		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const wsUrl = `${protocol}//${window.location.host}/ws/console`;

		try {
			this.socket = new WebSocket(wsUrl);
			this.socket.binaryType = 'arraybuffer';

			this.socket.onopen = () => {
				console.log('[WS] Connected');
				this.state.connectionState = 'connected';
				this.reconnectAttempts = 0;

				// Authenticate if we have a pending token
				if (this.pendingAuth) {
					this.authenticate(this.pendingAuth);
				}

				// Start ping timer
				this.startPingTimer();
			};

			this.socket.onclose = (event) => {
				console.log('[WS] Disconnected:', event.code, event.reason);
				this.cleanup();
				this.state.connectionState = 'disconnected';

				// Attempt reconnection if not a clean close
				if (event.code !== 1000) {
					this.scheduleReconnect();
				}
			};

			this.socket.onerror = (error) => {
				console.error('[WS] Error:', error);
				this.state.error = 'WebSocket connection error';
			};

			this.socket.onmessage = (event) => {
				this.handleMessage(event.data);
			};
		} catch (error) {
			console.error('[WS] Failed to connect:', error);
			this.state.connectionState = 'disconnected';
			this.state.error = 'Failed to establish connection';
			this.scheduleReconnect();
		}
	}

	disconnect(): void {
		if (this.socket) {
			this.socket.close(1000, 'Client disconnect');
			this.socket = null;
		}
		this.cleanup();
		this.state.connectionState = 'disconnected';
		this.subscriptions.clear();
	}

	private cleanup(): void {
		if (this.pingTimer) {
			clearInterval(this.pingTimer);
			this.pingTimer = null;
		}
		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}
	}

	private scheduleReconnect(): void {
		if (this.reconnectAttempts >= this.maxReconnectAttempts) {
			console.log('[WS] Max reconnect attempts reached');
			this.state.error = 'Unable to connect. Please refresh the page.';
			return;
		}

		const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts);
		this.reconnectAttempts++;

		console.log(`[WS] Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);
		this.reconnectTimer = setTimeout(() => {
			this.connect();
		}, delay);
	}

	private startPingTimer(): void {
		// Send ping every 30 seconds
		this.pingTimer = setInterval(() => {
			this.sendPing();
		}, 30000);
	}

	private handleMessage(data: ArrayBuffer): void {
		try {
			const msg = fromBinary(WebSocketServerMessageSchema, new Uint8Array(data));

			// Notify all message handlers
			this.messageHandlers.forEach((handler) => handler(msg));

			switch (msg.type) {
				case WSMessageType.WS_MESSAGE_TYPE_AUTH_OK:
					console.log('[WS] Authenticated');
					this.state.connectionState = 'authenticated';
					// Re-subscribe to all active subscriptions
					this.resubscribeAll();
					break;

				case WSMessageType.WS_MESSAGE_TYPE_AUTH_FAIL:
					console.error('[WS] Auth failed:', msg.payload.value);
					this.state.error = 'Authentication failed';
					break;

				case WSMessageType.WS_MESSAGE_TYPE_SUBSCRIBED:
					if (msg.payload.case === 'subscribed') {
						console.log('[WS] Subscribed to:', msg.payload.value.serverId);
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_UNSUBSCRIBED:
					if (msg.payload.case === 'unsubscribed') {
						console.log('[WS] Unsubscribed from:', msg.payload.value.serverId);
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_LOGS:
					if (msg.payload.case === 'logs') {
						const logsMsg = msg.payload.value as LogsMessage;
						this.logHandlers.forEach((handler) =>
							handler(logsMsg.serverId, logsMsg.logs)
						);
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_LOG:
					if (msg.payload.case === 'log') {
						const logMsg = msg.payload.value as LogMessage;
						if (logMsg.log) {
							this.logEntryHandlers.forEach((handler) =>
								handler(logMsg.serverId, logMsg.log!)
							);
						}
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_COMMAND_RESULT:
					if (msg.payload.case === 'commandResult') {
						const result = msg.payload.value as CommandResultMessage;
						this.commandResultHandlers.forEach((handler) => handler(result));
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_ERROR:
					if (msg.payload.case === 'error') {
						console.error('[WS] Server error:', msg.payload.value.error);
						this.state.error = msg.payload.value.error;
					}
					break;

				case WSMessageType.WS_MESSAGE_TYPE_PONG:
					// Pong received, connection is alive
					break;
			}
		} catch (error) {
			console.error('[WS] Failed to parse message:', error);
		}
	}

	private send(data: Uint8Array): boolean {
		if (this.socket?.readyState !== WebSocket.OPEN) {
			console.warn('[WS] Cannot send, socket not open');
			return false;
		}
		this.socket.send(data);
		return true;
	}

	authenticate(token: string): void {
		this.pendingAuth = token;

		if (this.state.connectionState !== 'connected' && this.state.connectionState !== 'authenticated') {
			// Will authenticate after connection
			return;
		}

		const msg = create(WebSocketClientMessageSchema, {
			type: WSMessageType.WS_MESSAGE_TYPE_AUTH,
			payload: {
				case: 'auth',
				value: create(AuthMessageSchema, { token })
			}
		});
		this.send(toBinary(WebSocketClientMessageSchema, msg));
	}

	subscribe(serverId: string, tail: number = 500): void {
		this.subscriptions.set(serverId, true);

		if (this.state.connectionState !== 'authenticated') {
			// Will subscribe after authentication
			return;
		}

		const msg = create(WebSocketClientMessageSchema, {
			type: WSMessageType.WS_MESSAGE_TYPE_SUBSCRIBE,
			payload: {
				case: 'subscribe',
				value: create(SubscribeMessageSchema, { serverId, tail })
			}
		});
		this.send(toBinary(WebSocketClientMessageSchema, msg));
	}

	unsubscribe(serverId: string): void {
		this.subscriptions.delete(serverId);

		if (this.state.connectionState !== 'authenticated') {
			return;
		}

		const msg = create(WebSocketClientMessageSchema, {
			type: WSMessageType.WS_MESSAGE_TYPE_UNSUBSCRIBE,
			payload: {
				case: 'unsubscribe',
				value: create(UnsubscribeMessageSchema, { serverId })
			}
		});
		this.send(toBinary(WebSocketClientMessageSchema, msg));
	}

	sendCommand(serverId: string, command: string): void {
		if (this.state.connectionState !== 'authenticated') {
			console.warn('[WS] Cannot send command, not authenticated');
			return;
		}

		const msg = create(WebSocketClientMessageSchema, {
			type: WSMessageType.WS_MESSAGE_TYPE_COMMAND,
			payload: {
				case: 'command',
				value: create(CommandMessageSchema, { serverId, command })
			}
		});
		this.send(toBinary(WebSocketClientMessageSchema, msg));
	}

	private sendPing(): void {
		if (this.state.connectionState !== 'authenticated' && this.state.connectionState !== 'connected') {
			return;
		}

		const msg = create(WebSocketClientMessageSchema, {
			type: WSMessageType.WS_MESSAGE_TYPE_PING,
			payload: { case: undefined, value: undefined }
		});
		this.send(toBinary(WebSocketClientMessageSchema, msg));
	}

	private resubscribeAll(): void {
		for (const serverId of this.subscriptions.keys()) {
			const msg = create(WebSocketClientMessageSchema, {
				type: WSMessageType.WS_MESSAGE_TYPE_SUBSCRIBE,
				payload: {
					case: 'subscribe',
					value: create(SubscribeMessageSchema, { serverId, tail: 500 })
				}
			});
			this.send(toBinary(WebSocketClientMessageSchema, msg));
		}
	}

	// Event handler registration
	onMessage(handler: MessageHandler): () => void {
		this.messageHandlers.add(handler);
		return () => this.messageHandlers.delete(handler);
	}

	onLogs(handler: LogHandler): () => void {
		this.logHandlers.add(handler);
		return () => this.logHandlers.delete(handler);
	}

	onLogEntry(handler: LogEntryHandler): () => void {
		this.logEntryHandlers.add(handler);
		return () => this.logEntryHandlers.delete(handler);
	}

	onCommandResult(handler: CommandResultHandler): () => void {
		this.commandResultHandlers.add(handler);
		return () => this.commandResultHandlers.delete(handler);
	}

	// Check if connected and authenticated
	get isReady(): boolean {
		return this.state.connectionState === 'authenticated';
	}
}

export const wsClient = new WebSocketClient();
