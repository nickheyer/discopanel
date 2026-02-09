import type { CallOptions } from '@connectrpc/connect';
import { authStore } from '$lib/stores/auth';

export interface StreamOptions {
  onError?: (error: Error) => void;
  reconnectDelay?: number; // ms, default 3000
  maxReconnects?: number; // default 10
}

/**
 * Creates call options for streaming RPCs with auth headers and abort signal.
 */
function streamCallOptions(signal: AbortSignal): CallOptions {
  const headers = authStore.getHeaders();
  const h = new Headers();
  Object.entries(headers).forEach(([key, value]) => {
    h.set(key, value as string);
  });
  return { signal, headers: h };
}

/**
 * Consumes a ConnectRPC server stream with auto-reconnection.
 * Returns a cleanup function to stop the stream.
 *
 * @param streamFn - Function that creates the stream given call options
 * @param onMessage - Callback for each streamed message
 * @param options - Reconnection and error handling options
 */
export function consumeStream<T>(
  streamFn: (options: CallOptions) => AsyncIterable<T>,
  onMessage: (msg: T) => void,
  options: StreamOptions = {}
): () => void {
  const { reconnectDelay = 3000, maxReconnects = 10, onError } = options;
  let abortController = new AbortController();
  let reconnectCount = 0;
  let stopped = false;

  async function connect() {
    while (!stopped && reconnectCount <= maxReconnects) {
      try {
        abortController = new AbortController();
        const callOpts = streamCallOptions(abortController.signal);
        const stream = streamFn(callOpts);
        reconnectCount = 0; // reset on successful connection

        for await (const msg of stream) {
          if (stopped) return;
          onMessage(msg);
        }
        // Stream ended cleanly (server closed it)
        if (stopped) return;
      } catch (err: any) {
        if (stopped || err.name === 'AbortError') return;
        reconnectCount++;
        onError?.(err);
        if (reconnectCount > maxReconnects) {
          console.error('Stream: max reconnects exceeded');
          return;
        }
        // Backoff: delay increases with each retry
        await new Promise((r) => setTimeout(r, reconnectDelay * Math.min(reconnectCount, 5)));
      }
    }
  }

  connect();

  // Return cleanup function
  return () => {
    stopped = true;
    abortController.abort();
  };
}
