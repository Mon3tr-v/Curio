import type {
  Batch,
  AuthLoginResult,
  AuthStatus,
  ClassificationConfig,
  Collection,
  CollectionPage,
  CloudDriveFile,
  CloudDriveSettings,
  CloudDriveStatus,
  DirectoryConfig,
  Health,
  MediaFile,
  MediaFilePage,
  MediaStats,
  NamingTemplate,
  P115AuthResult,
  P115OAuthStart,
  P115QRCodeSession,
  P115QRCodeStatus,
  P115Settings,
  P115SyncRun,
  P115Status,
  RearchiveBatchResult,
  RearchivePayload,
  STRMSyncResult,
  SystemSettings,
  TVShow,
  TVShowPage,
} from './types';

export const API_URL = import.meta.env.VITE_API_URL ?? '';
export const AUTH_TOKEN_KEY = 'curio.adminToken';

export class CurioAuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CurioAuthError';
  }
}

export function getAuthToken() {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
}

export function setAuthToken(token: string) {
  if (typeof window === 'undefined') return;
  const next = token.trim();
  if (next) {
    window.localStorage.setItem(AUTH_TOKEN_KEY, next);
  } else {
    window.localStorage.removeItem(AUTH_TOKEN_KEY);
  }
}

export function isAuthError(error: unknown) {
  return error instanceof CurioAuthError;
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getAuthToken();
  let response: Response;
  try {
    response = await fetch(`${API_URL}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { 'X-Curio-Token': token } : {}),
        ...(init?.headers ?? {}),
      },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : '网络请求失败';
    throw new Error(`无法连接后端接口 ${path}：${message}`);
  }
  if (!response.ok) {
    const payload = await response.json().catch(() => ({ error: response.statusText }));
    if (response.status === 401) {
      throw new CurioAuthError(payload.error ?? '需要 Curio 管理令牌');
    }
    throw new Error(payload.error ?? response.statusText);
  }
  return response.json() as Promise<T>;
}

type MediaListParams = { query?: string; limit?: number; offset?: number; status?: string };

function withMediaParams(path: string, params: MediaListParams = {}) {
  const query = new URLSearchParams();
  if (params.query?.trim()) query.set('q', params.query.trim());
  if (params.limit) query.set('limit', String(params.limit));
  if (params.offset) query.set('offset', String(params.offset));
  if (params.status?.trim()) query.set('status', params.status.trim());
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

export const endpoints = {
  authStatus: () => api<AuthStatus>('/api/auth/status'),
  authLogin: (token: string) => api<AuthLoginResult>('/api/auth/login', { method: 'POST', body: JSON.stringify({ token }) }),
  health: () => api<Health>('/api/health'),
  batches: () => api<Batch[]>('/api/batches'),
  stats: () => api<MediaStats>('/api/stats'),
  activeTask: () => api<Batch | null>('/api/tasks/active'),
  stopTask: (batchID: string) => api<Batch>(`/api/tasks/${batchID}/stop`, { method: 'POST' }),
  startScan: () => api<{ batch_id: string; status: string }>('/api/scan/start', { method: 'POST' }),
  startCloudDriveScan: () => api<{ batch_id: string; status: string }>('/api/scan/clouddrive/start', { method: 'POST' }),
  directories: () => api<DirectoryConfig>('/api/settings/directories'),
  saveDirectories: (payload: unknown) =>
    api<DirectoryConfig>('/api/settings/directories', { method: 'PUT', body: JSON.stringify(payload) }),
  systemSettings: () => api<SystemSettings>('/api/settings/system'),
  saveSystemSettings: (payload: unknown) =>
    api<SystemSettings>('/api/settings/system', { method: 'PUT', body: JSON.stringify(payload) }),
  cloudDriveSettings: () => api<CloudDriveSettings>('/api/settings/clouddrive'),
  saveCloudDriveSettings: (payload: unknown) =>
    api<CloudDriveSettings>('/api/settings/clouddrive', { method: 'PUT', body: JSON.stringify(payload) }),
  testCloudDrive: () => api<CloudDriveStatus>('/api/clouddrive/test', { method: 'POST' }),
  cloudDriveFiles: (path: string) => api<CloudDriveFile[]>(`/api/clouddrive/files?path=${encodeURIComponent(path)}`),
  p115Settings: () => api<P115Settings>('/api/settings/p115'),
  saveP115Settings: (payload: unknown) =>
    api<P115Settings>('/api/settings/p115', { method: 'PUT', body: JSON.stringify(payload) }),
  startP115QRCode: () => api<P115QRCodeSession>('/api/p115/auth/qrcode/start', { method: 'POST' }),
  p115QRCodeStatus: (uid: string) => api<P115QRCodeStatus>(`/api/p115/auth/qrcode/${encodeURIComponent(uid)}/status`),
  completeP115QRCode: (uid: string) =>
    api<P115AuthResult>('/api/p115/auth/qrcode/complete', { method: 'POST', body: JSON.stringify({ uid }) }),
  startP115OAuth: () => api<P115OAuthStart>('/api/p115/auth/oauth/start', { method: 'POST' }),
  importP115Token: (payload: unknown) =>
    api<P115AuthResult>('/api/p115/auth/import-token', { method: 'POST', body: JSON.stringify(payload) }),
  refreshP115Token: () => api<P115AuthResult>('/api/p115/auth/refresh', { method: 'POST' }),
  testP115: () => api<P115Status>('/api/p115/test', { method: 'POST' }),
  exportP115Tree: () => api<STRMSyncResult>('/api/p115/export-tree', { method: 'POST' }),
  syncP115STRM: () => api<STRMSyncResult>('/api/p115/strm/sync', { method: 'POST' }),
  cleanupP115STRM: () => api<STRMSyncResult>('/api/p115/strm/cleanup', { method: 'POST' }),
  p115SyncRuns: () => api<P115SyncRun[]>('/api/p115/sync-runs?limit=20'),
  classification: () => api<ClassificationConfig>('/api/settings/classification'),
  saveClassification: (payload: unknown) =>
    api<ClassificationConfig>('/api/settings/classification', { method: 'PUT', body: JSON.stringify(payload) }),
  templates: () => api<NamingTemplate[]>('/api/settings/templates'),
  saveTemplate: (type: string, payload: unknown) =>
    api<NamingTemplate>(`/api/settings/templates/${type}`, { method: 'PUT', body: JSON.stringify(payload) }),
  previewTemplate: (payload: unknown) =>
    api<{ preview: string }>('/api/settings/templates/preview', { method: 'POST', body: JSON.stringify(payload) }),
  mediaFiles: (params?: MediaListParams) => api<MediaFilePage>(withMediaParams('/api/media-files', params)),
  staging: (params?: MediaListParams) => api<MediaFilePage>(withMediaParams('/api/staging', params)),
  failed: (params?: MediaListParams) => api<MediaFilePage>(withMediaParams('/api/failed', params)),
  deleteMediaFile: (id: string) => api<{ status: string }>(`/api/media-files/${id}`, { method: 'DELETE' }),
  deleteMediaFiles: (ids: string[]) =>
    api<{ status: string; count: number }>('/api/media-files/bulk-delete', { method: 'POST', body: JSON.stringify({ file_ids: ids }) }),
  rearchiveMediaFile: (id: string, payload: RearchivePayload) =>
    api<MediaFile>(`/api/media-files/${id}/rearchive`, { method: 'POST', body: JSON.stringify(payload) }),
  rearchiveMediaFiles: (ids: string[], payload: RearchivePayload) =>
    api<RearchiveBatchResult>('/api/media-files/bulk-rearchive', {
      method: 'POST',
      body: JSON.stringify({ file_ids: ids, ...payload }),
    }),
  tvShows: (params?: MediaListParams) => api<TVShowPage>(withMediaParams('/api/tv-shows', params)),
  tvShow: (id: number) => api<TVShow>(`/api/tv-shows/${id}`),
  collections: (params?: MediaListParams) => api<CollectionPage>(withMediaParams('/api/collections', params)),
  collection: (id: number) => api<Collection>(`/api/collections/${id}`),
};
