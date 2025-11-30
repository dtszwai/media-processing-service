import type {
  Media,
  InitUploadRequest,
  InitUploadResponse,
  StatusResponse,
  UploadResponse,
  ResizeRequest,
  OutputFormat,
} from './types';

const API_BASE = '/api';

// Threshold for using presigned URL upload (5MB)
export const PRESIGNED_UPLOAD_THRESHOLD = 5 * 1024 * 1024;

export async function checkHealth(): Promise<boolean> {
  try {
    const response = await fetch(`${API_BASE}/health`);
    return response.ok;
  } catch {
    return false;
  }
}

export async function getAllMedia(): Promise<Media[]> {
  const response = await fetch(API_BASE);
  if (!response.ok) throw new Error('Failed to fetch media');
  return response.json();
}

export async function getMediaStatus(mediaId: string): Promise<StatusResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/status`);
  if (response.status === 404) {
    throw new Error('NOT_FOUND');
  }
  if (!response.ok) throw new Error('Failed to fetch status');
  return response.json();
}

export async function uploadMedia(
  file: File,
  width: number,
  outputFormat: OutputFormat = 'jpeg'
): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append('file', file);
  formData.append('width', width.toString());
  formData.append('outputFormat', outputFormat);

  const response = await fetch(`${API_BASE}/upload`, {
    method: 'POST',
    body: formData,
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Upload failed');
  }

  return response.json();
}

export async function initPresignedUpload(
  request: InitUploadRequest
): Promise<InitUploadResponse> {
  const response = await fetch(`${API_BASE}/upload/init`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to initialize upload');
  }

  return response.json();
}

export async function uploadToPresignedUrl(
  url: string,
  file: File,
  headers: Record<string, string>,
  onProgress?: (progress: number) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', url);

    Object.entries(headers).forEach(([key, value]) => {
      xhr.setRequestHeader(key, value);
    });

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) {
        const progress = (event.loaded / event.total) * 100;
        onProgress(progress);
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve();
      } else {
        reject(new Error(`Upload failed with status ${xhr.status}`));
      }
    };

    xhr.onerror = () => reject(new Error('Upload failed'));
    xhr.send(file);
  });
}

export async function completePresignedUpload(
  mediaId: string
): Promise<UploadResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/upload/complete`, {
    method: 'POST',
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Failed to complete upload');
  }

  return response.json();
}

export async function resizeMedia(
  mediaId: string,
  request: ResizeRequest
): Promise<void> {
  const response = await fetch(`${API_BASE}/${mediaId}/resize`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });

  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.message || 'Resize failed');
  }
}

export async function deleteMedia(mediaId: string): Promise<void> {
  const response = await fetch(`${API_BASE}/${mediaId}`, {
    method: 'DELETE',
  });

  if (!response.ok) throw new Error('Delete failed');
}

export function getDownloadUrl(mediaId: string): string {
  return `${API_BASE}/${mediaId}/download`;
}

export function getOriginalUrl(mediaId: string, fileName: string): string {
  return `http://127.0.0.1:4566/media-bucket/uploads/${mediaId}/${fileName}`;
}

export async function pollForStatus(
  mediaId: string,
  targetStatuses: string[],
  onStatusChange?: (status: string) => void,
  interval = 2000
): Promise<string> {
  while (true) {
    try {
      const { status } = await getMediaStatus(mediaId);
      onStatusChange?.(status);

      if (targetStatuses.includes(status)) {
        return status;
      }

      if (status === 'ERROR') {
        throw new Error('Processing failed');
      }
    } catch (error) {
      if (error instanceof Error && error.message === 'NOT_FOUND') {
        return 'DELETED';
      }
      throw error;
    }

    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}
