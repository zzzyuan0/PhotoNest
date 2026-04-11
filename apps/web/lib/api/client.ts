import type { components } from './schema';

export type ApiError = components['schemas']['Error'];
export type AuthSession = components['schemas']['AuthSession'];
export type AuthSessionResponse = components['schemas']['AuthSessionResponse'];
export type AssetAcceptedResponse = components['schemas']['AssetAcceptedResponse'];
export type ConfirmUploadRequest = components['schemas']['ConfirmUploadRequest'];
export type CreateImportSessionRequest = components['schemas']['CreateImportSessionRequest'];
export type CreateUploadTicketRequest = components['schemas']['CreateUploadTicketRequest'];
export type HealthResponse = components['schemas']['HealthResponse'];
export type ImportSession = components['schemas']['ImportSession'];
export type SearchResponse = components['schemas']['SearchResponse'];
export type TimelineResponse = components['schemas']['TimelineResponse'];
export type UploadTicket = components['schemas']['UploadTicket'];

export type ApiFetchOptions = Omit<RequestInit, 'body'> & {
  body?: unknown;
};

export async function apiFetch<T>(path: string, init?: ApiFetchOptions): Promise<T> {
  const config = useRuntimeConfig();
  const headers = new Headers(init?.headers);
  const csrfToken = useCookie<string | null>('photonest_csrf').value;

  if (csrfToken && !headers.has('X-CSRF-Token')) {
    headers.set('X-CSRF-Token', csrfToken);
  }

  return await $fetch<T>(path, {
    baseURL: config.public.apiBaseURL,
    credentials: 'include',
    headers,
    ...init,
  });
}
