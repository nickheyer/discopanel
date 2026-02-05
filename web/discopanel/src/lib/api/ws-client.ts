import { fromBinary, toBinary, create, type DescMessage, type MessageShape } from '@bufbuild/protobuf';
import { WsMessageSchema, WsMessageType } from '$lib/proto/discopanel/v1/ws_pb';
import { authStore } from '$lib/stores/auth';

type Subscription = {
  schema: DescMessage;
  callback: (data: any) => void;
};

class WsClient {
  private ws: WebSocket | null = null;
  private subscriptions = new Map<string, Subscription>();
  private reconnectAttempts = 0;
  private maxReconnectDelay = 30000;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  connect(): void {
    if (this.ws && (this.ws.readyState === WebSocket.CONNECTING || this.ws.readyState === WebSocket.OPEN)) {
      return;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    this.ws = new WebSocket(`${protocol}//${window.location.host}/api/ws`);
    this.ws.binaryType = 'arraybuffer';

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.resubscribeAll();
    };

    this.ws.onmessage = (event) => {
      const msg = fromBinary(WsMessageSchema, new Uint8Array(event.data as ArrayBuffer));
      if (msg.type === WsMessageType.UPDATE) {
        const sub = this.subscriptions.get(msg.topic);
        if (sub) {
          const decoded = fromBinary(sub.schema as DescMessage, msg.payload);
          sub.callback(decoded);
        }
      }
    };

    this.ws.onclose = () => {
      this.ws = null;
      this.scheduleReconnect();
    };

    this.ws.onerror = () => {
      // onclose will fire after this
    };
  }

  subscribe<T extends DescMessage>(
    topic: string,
    token: string,
    schema: T,
    callback: (data: MessageShape<T>) => void,
  ): void {
    this.subscriptions.set(topic, { schema, callback: callback as (data: any) => void });
    this.sendSubscribe(topic, token);
  }

  unsubscribe(topic: string): void {
    this.subscriptions.delete(topic);
    this.sendUnsubscribe(topic);
  }

  disconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.subscriptions.clear();
  }

  private sendSubscribe(topic: string, token: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const msg = toBinary(WsMessageSchema, create(WsMessageSchema, {
      type: WsMessageType.SUBSCRIBE,
      topic,
      token,
    }));
    this.ws.send(msg);
  }

  private sendUnsubscribe(topic: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    const msg = toBinary(WsMessageSchema, create(WsMessageSchema, {
      type: WsMessageType.UNSUBSCRIBE,
      topic,
    }));
    this.ws.send(msg);
  }

  private scheduleReconnect(): void {
    if (this.subscriptions.size === 0) return;
    const delay = Math.min(1000 * 2 ** this.reconnectAttempts, this.maxReconnectDelay);
    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private resubscribeAll(): void {
    const token = authStore.getToken();
    for (const [topic] of this.subscriptions) {
      this.sendSubscribe(topic, token);
    }
  }
}

export const wsClient = new WsClient();
