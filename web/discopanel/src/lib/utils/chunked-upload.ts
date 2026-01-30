import { rpcClient, silentCallOptions } from '$lib/api/rpc-client';

export interface UploadProgress {
  sessionId: string;
  bytesUploaded: number;
  totalBytes: number;
  chunksUploaded: number;
  totalChunks: number;
  percentComplete: number;
}

export interface ChunkedUploadOptions {
  chunkSize?: number;
  onProgress?: (progress: UploadProgress) => void;
  signal?: AbortSignal;
  sessionId?: string; // For resume
}

export interface UploadResult {
  sessionId: string;
  tempPath: string;
}

export interface UploadStatus extends UploadProgress {
  missingChunks: number[];
  completed: boolean;
  tempPath?: string;
}

// NOTE: Optional to init session, client can override server default up to max set by server
const DEFAULT_CHUNK_SIZE = 5 * 1024 * 1024; // 5MB
const SESSION_STORAGE_KEY = 'chunked_upload_sessions';

// Store session info in localStorage for resume capability
function saveSessionToStorage(sessionId: string, filename: string, totalSize: number): void {
  try {
    const sessions = JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) || '{}');
    sessions[filename] = { sessionId, totalSize, timestamp: Date.now() };
    localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(sessions));
  } catch {
    // Ignore localStorage errors
  }
}

function getSessionFromStorage(filename: string, totalSize: number): string | null {
  try {
    const sessions = JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) || '{}');
    const session = sessions[filename];
    // Check if session exists, matches size, and isn't too old (4 hours)
    if (session && session.totalSize === totalSize && Date.now() - session.timestamp < 4 * 60 * 60 * 1000) {
      return session.sessionId;
    }
  } catch {
    // Ignore localStorage errors
  }
  return null;
}

function removeSessionFromStorage(filename: string): void {
  try {
    const sessions = JSON.parse(localStorage.getItem(SESSION_STORAGE_KEY) || '{}');
    delete sessions[filename];
    localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(sessions));
  } catch {
    // Ignore localStorage errors
  }
}

/**
 * Upload a file using chunked upload with resumability support
 */
export async function uploadFile(
  file: File,
  options?: ChunkedUploadOptions
): Promise<UploadResult> {
  const chunkSize = options?.chunkSize || DEFAULT_CHUNK_SIZE;
  const onProgress = options?.onProgress;
  const signal = options?.signal;

  let sessionId = options?.sessionId;
  let missingChunks: number[] = [];

  // Check for existing session if not explicitly provided
  if (!sessionId) {
    sessionId = getSessionFromStorage(file.name, file.size) || undefined;
  }

  // If we have an existing session, check its status
  if (sessionId) {
    try {
      const status = await getUploadStatus(sessionId);
      if (status.completed) {
        removeSessionFromStorage(file.name);
        return { sessionId, tempPath: status.tempPath || '' };
      }
      missingChunks = status.missingChunks;
    } catch {
      // Session expired or invalid, start fresh
      sessionId = undefined;
    }
  }

  // Initialize new session if needed
  if (!sessionId) {
    const initResponse = await rpcClient.upload.initUpload({
      filename: file.name,
      totalSize: BigInt(file.size),
      chunkSize: chunkSize
    }, silentCallOptions);
    sessionId = initResponse.sessionId;
    saveSessionToStorage(sessionId, file.name, file.size);
  }

  const totalChunks = Math.ceil(file.size / chunkSize);

  // Determine which chunks to upload
  const chunksToUpload = missingChunks.length > 0
    ? missingChunks
    : Array.from({ length: totalChunks }, (_, i) => i);

  let chunksUploaded = totalChunks - chunksToUpload.length;
  let bytesUploaded = chunksUploaded * chunkSize;
  // Adjust for last chunk which might be smaller
  if (chunksUploaded > 0 && chunksUploaded < totalChunks) {
    bytesUploaded = Math.min(bytesUploaded, file.size);
  }

  // Report initial progress
  if (onProgress) {
    onProgress({
      sessionId,
      bytesUploaded,
      totalBytes: file.size,
      chunksUploaded,
      totalChunks,
      percentComplete: (bytesUploaded / file.size) * 100
    });
  }

  // Upload chunks
  for (const chunkIndex of chunksToUpload) {
    // Check for abort
    if (signal?.aborted) {
      throw new Error('Upload cancelled');
    }

    const start = chunkIndex * chunkSize;
    const end = Math.min(start + chunkSize, file.size);
    const chunk = file.slice(start, end);
    const chunkData = new Uint8Array(await chunk.arrayBuffer());

    const response = await rpcClient.upload.uploadChunk({
      sessionId,
      chunkIndex,
      data: chunkData
    }, silentCallOptions);

    chunksUploaded++;
    bytesUploaded = Number(response.bytesReceived);

    // Report progress
    if (onProgress) {
      onProgress({
        sessionId,
        bytesUploaded,
        totalBytes: file.size,
        chunksUploaded: response.chunksReceived,
        totalChunks,
        percentComplete: (bytesUploaded / file.size) * 100
      });
    }

    // Check if completed
    if (response.completed) {
      removeSessionFromStorage(file.name);
      return {
        sessionId,
        tempPath: response.tempPath
      };
    }
  }

  // Final status check
  const finalStatus = await getUploadStatus(sessionId);
  if (finalStatus.completed) {
    removeSessionFromStorage(file.name);
    return {
      sessionId,
      tempPath: finalStatus.tempPath || ''
    };
  }

  throw new Error('Upload did not complete successfully');
}

/**
 * Check the status of an upload session
 */
export async function getUploadStatus(sessionId: string): Promise<UploadStatus> {
  const response = await rpcClient.upload.getUploadStatus({ sessionId }, silentCallOptions);

  return {
    sessionId: response.sessionId,
    bytesUploaded: Number(response.bytesReceived),
    totalBytes: Number(response.totalBytes),
    chunksUploaded: response.chunksReceived,
    totalChunks: response.totalChunks,
    percentComplete: (Number(response.bytesReceived) / Number(response.totalBytes)) * 100,
    missingChunks: response.missingChunks,
    completed: response.completed,
    tempPath: response.tempPath
  };
}

/**
 * Cancel an upload session
 */
export async function cancelUpload(sessionId: string): Promise<void> {
  await rpcClient.upload.cancelUpload({ sessionId }, silentCallOptions);
}
