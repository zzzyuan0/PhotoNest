import type { components } from './schema';

export type ApiError = components['schemas']['Error'];
export type HealthResponse = components['schemas']['HealthResponse'];
export type TimelineResponse = components['schemas']['TimelineResponse'];

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const config = useRuntimeConfig();

  return await $fetch<T>(path, {
    baseURL: config.public.apiBaseURL,
    ...init,
  });
}
