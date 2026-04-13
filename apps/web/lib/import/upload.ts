import type { AssetAcceptedResponse, UploadTicket } from '../api/client';

export type CompletedPart = {
  partNumber: number;
  etag: string;
};

export type ConfirmUploadInput = {
  objectKey: string;
  contentLength: number;
  contentSha256: string;
  etag?: string;
  uploadId?: string;
  parts?: CompletedPart[];
};

export type ExecuteUploadFlowInput = {
  ticket: UploadTicket;
  file: File;
  contentSha256: string;
  confirmUpload: (payload: ConfirmUploadInput) => Promise<AssetAcceptedResponse>;
  fetchImpl?: typeof fetch;
};

export async function executeUploadFlow({
  ticket,
  file,
  contentSha256,
  confirmUpload,
  fetchImpl = fetch,
}: ExecuteUploadFlowInput): Promise<AssetAcceptedResponse> {
  let completedParts: CompletedPart[] | undefined;
  let uploadId: string | undefined;
  let uploadETag: string | undefined;

  if (ticket.multipart) {
    const multipartResult = await uploadMultipart(ticket, file, fetchImpl);
    completedParts = multipartResult.parts;
    uploadId = ticket.multipart.uploadId;
  } else {
    uploadETag = await uploadSingle(ticket, file, fetchImpl);
  }

  return await confirmUpload({
    objectKey: ticket.objectKey,
    contentLength: file.size,
    contentSha256,
    etag: uploadETag,
    uploadId,
    parts: completedParts,
  });
}

export async function uploadSingle(
  ticket: UploadTicket,
  file: File,
  fetchImpl: typeof fetch = fetch,
): Promise<string | undefined> {
  if (!ticket.url) {
    throw new Error('服务端没有返回可用的上传地址');
  }

  const uploadResponse = await fetchObjectStorage(ticket.url, {
    method: ticket.method || 'PUT',
    headers: buildUploadHeaders(ticket.headers, file, true),
    body: file,
  }, fetchImpl);
  if (!uploadResponse.ok) {
    throw new Error(`对象上传失败，状态码 ${uploadResponse.status}`);
  }

  return uploadResponse.headers.get('etag') ?? undefined;
}

export async function uploadMultipart(
  ticket: UploadTicket,
  file: File,
  fetchImpl: typeof fetch = fetch,
): Promise<{ parts: CompletedPart[] }> {
  if (!ticket.multipart || ticket.multipart.parts.length === 0) {
    throw new Error('服务端没有返回有效的 multipart 票据');
  }

  const partSize = Math.ceil(file.size / ticket.multipart.parts.length);
  const parts: CompletedPart[] = [];

  for (const part of ticket.multipart.parts) {
    const start = (part.partNumber - 1) * partSize;
    const end = Math.min(file.size, start + partSize);
    const chunk = file.slice(start, end);
    const response = await fetchObjectStorage(part.uploadUrl, {
      method: 'PUT',
      headers: buildUploadHeaders(part.headers, file, false),
      body: chunk,
    }, fetchImpl);
    if (!response.ok) {
      throw new Error(`分片 ${part.partNumber} 上传失败，状态码 ${response.status}`);
    }

    const etag = response.headers.get('etag');
    if (!etag) {
      throw new Error(`分片 ${part.partNumber} 上传成功，但响应里缺少 etag`);
    }

    parts.push({
      partNumber: part.partNumber,
      etag,
    });
  }

  return { parts };
}

export async function fetchObjectStorage(
  url: string,
  init: RequestInit,
  fetchImpl: typeof fetch = fetch,
): Promise<Response> {
  try {
    return await fetchImpl(url, init);
  } catch (error) {
    throw describeObjectStorageFetchError(url, error);
  }
}

export function buildUploadHeaders(
  rawHeaders: Record<string, string> | undefined,
  file: File,
  includeContentType: boolean,
): Headers {
  const headers = new Headers();
  for (const [name, value] of Object.entries(rawHeaders ?? {})) {
    const lowered = name.toLowerCase();
    if (lowered === 'host' || lowered === 'content-length') {
      continue;
    }
    headers.set(name, value);
  }

  if (includeContentType && !headers.has('Content-Type')) {
    headers.set('Content-Type', file.type || 'application/octet-stream');
  }

  return headers;
}

export function describeObjectStorageFetchError(url: string, error: unknown): Error {
  if (!(error instanceof Error)) {
    return new Error('对象存储直传失败，请检查对象存储地址、跨域配置与网络连通性');
  }

  if (error.message !== 'Failed to fetch') {
    return error;
  }

  let targetOrigin = url;
  try {
    targetOrigin = new URL(url).origin;
  } catch {
    // Keep the original URL when parsing fails.
  }

  const currentOrigin = typeof window !== 'undefined' ? window.location.origin : '当前页面';
  return new Error(
    `对象存储直传失败：浏览器无法访问 ${targetOrigin}。请检查对象存储 CORS 是否允许 ${currentOrigin}，并确认预签名地址中的主机名对当前浏览器所在设备可达。`,
  );
}
