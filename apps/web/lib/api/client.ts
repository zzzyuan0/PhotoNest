import type { components } from './schema';

export type ApiError = components['schemas']['Error'];
export type AuthSession = components['schemas']['AuthSession'];
export type AuthSessionResponse = components['schemas']['AuthSessionResponse'];
export type AssetAcceptedResponse = components['schemas']['AssetAcceptedResponse'];
export type AssetDetailResponse = components['schemas']['AssetDetailResponse'];
export type AlbumDetailResponse = components['schemas']['AlbumDetailResponse'];
export type AlbumSummary = components['schemas']['AlbumSummary'];
export type AlbumsResponse = components['schemas']['AlbumsResponse'];
export type ConfirmUploadRequest = components['schemas']['ConfirmUploadRequest'];
export type CreateAlbumRequest = components['schemas']['CreateAlbumRequest'];
export type CreateImportSessionRequest = components['schemas']['CreateImportSessionRequest'];
export type CreateUploadTicketRequest = components['schemas']['CreateUploadTicketRequest'];
export type DuplicatesResponse = components['schemas']['DuplicatesResponse'];
export type DownloadGrant = components['schemas']['DownloadGrant'];
export type ExportJob = components['schemas']['ExportJob'];
export type ExportRequest = components['schemas']['ExportRequest'];
export type HealthResponse = components['schemas']['HealthResponse'];
export type ImportSession = components['schemas']['ImportSession'];
export type PlacesResponse = components['schemas']['PlacesResponse'];
export type SearchResponse = components['schemas']['SearchResponse'];
export type SetFavoriteRequest = components['schemas']['SetFavoriteRequest'];
export type TimelineResponse = components['schemas']['TimelineResponse'];
export type UploadTicket = components['schemas']['UploadTicket'];

export type ApiFetchOptions = Omit<RequestInit, 'body'> & {
  body?: unknown;
};

function resolveApiBaseURL(configuredBaseURL: string): string {
  if (!import.meta.client) {
    return configuredBaseURL;
  }

  const trimmed = configuredBaseURL.trim();
  if (trimmed === '') {
    return `${window.location.protocol}//${window.location.hostname}:8080`;
  }

  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return trimmed;
  }

  const localHosts = new Set(['localhost', '127.0.0.1', '::1']);
  if (!localHosts.has(parsed.hostname) || localHosts.has(window.location.hostname)) {
    return trimmed;
  }

  parsed.protocol = window.location.protocol;
  parsed.hostname = window.location.hostname;
  parsed.port = '8080';
  return parsed.toString().replace(/\/$/, '');
}

export async function apiFetch<T>(path: string, init?: ApiFetchOptions): Promise<T> {
  const config = useRuntimeConfig();
  const headers = new Headers(init?.headers);
  const csrfToken = useCookie<string | null>('photonest_csrf').value;

  if (csrfToken && !headers.has('X-CSRF-Token')) {
    headers.set('X-CSRF-Token', csrfToken);
  }

  return await $fetch<T>(path, {
    baseURL: resolveApiBaseURL(config.public.apiBaseURL),
    credentials: 'include',
    headers,
    ...init,
  });
}
