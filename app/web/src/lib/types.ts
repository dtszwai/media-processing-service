export type MediaStatus =
  | 'PENDING_UPLOAD'
  | 'PENDING'
  | 'PROCESSING'
  | 'COMPLETE'
  | 'ERROR'
  | 'DELETING';

export type OutputFormat = 'jpeg' | 'png' | 'webp';

export interface Media {
  mediaId: string;
  name: string;
  size: number;
  mimetype: string;
  status: MediaStatus;
  width: number;
  outputFormat?: OutputFormat;
  createdAt?: string;
  updatedAt?: string;
}

export interface InitUploadRequest {
  fileName: string;
  fileSize: number;
  contentType: string;
  width?: number;
  outputFormat?: OutputFormat;
}

export interface InitUploadResponse {
  mediaId: string;
  uploadUrl: string;
  expiresIn: number;
  method: string;
  headers: Record<string, string>;
}

export interface StatusResponse {
  status: MediaStatus;
}

export interface UploadResponse {
  mediaId: string;
}

export interface ResizeRequest {
  width: number;
  outputFormat?: OutputFormat;
}
