import { describe, expect, it, vi } from 'vitest';

import type { UploadTicket } from '../api/client';
import { describeObjectStorageFetchError, executeUploadFlow } from './upload';

describe('executeUploadFlow', () => {
  it('在单次上传分支里回传 etag 并触发确认', async () => {
    const file = new File(['hello world'], 'single.jpg', { type: 'image/jpeg' });
    const ticket: UploadTicket = {
      objectKey: 'imports/single.jpg',
      provider: 'cos',
      method: 'PUT',
      url: 'https://cos.example.com/single.jpg',
      headers: {
        host: 'cos.example.com',
        'content-length': '11',
        'x-cos-meta-origin': 'web',
      },
      expiresAt: '2026-04-12T00:00:00Z',
      multipart: false,
    };
    const fetchImpl = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(null, {
        status: 200,
        headers: {
          etag: '"etag-single"',
        },
      }),
    );
    const confirmUpload = vi.fn().mockResolvedValue({
      assetId: 'asset-single',
      processingStage: 'queued',
    });

    const confirmation = await executeUploadFlow({
      ticket,
      file,
      contentSha256: 'sha-single',
      fetchImpl,
      confirmUpload,
    });

    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(fetchImpl.mock.calls[0]?.[0]).toBe(ticket.url);
    expect(fetchImpl.mock.calls[0]?.[1]?.method).toBe('PUT');

    const headers = fetchImpl.mock.calls[0]?.[1]?.headers as Headers;
    expect(headers.get('Content-Type')).toBe('image/jpeg');
    expect(headers.get('x-cos-meta-origin')).toBe('web');
    expect(headers.has('host')).toBe(false);
    expect(headers.has('content-length')).toBe(false);

    expect(confirmUpload).toHaveBeenCalledWith({
      objectKey: 'imports/single.jpg',
      contentLength: 11,
      contentSha256: 'sha-single',
      etag: '"etag-single"',
      uploadId: undefined,
      parts: undefined,
    });
    expect(confirmation).toEqual({
      assetId: 'asset-single',
      processingStage: 'queued',
    });
  });

  it('在 multipart 分支里按顺序上传分片并提交 uploadId 与 parts', async () => {
    const file = new File(['abcdefghi'], 'multi.jpg', { type: 'image/jpeg' });
    const ticket: UploadTicket = {
      objectKey: 'imports/multi.jpg',
      provider: 'cos',
      method: 'PUT',
      expiresAt: '2026-04-12T00:00:00Z',
      multipart: {
        uploadId: 'upload-123',
        parts: [
          {
            partNumber: 1,
            uploadUrl: 'https://cos.example.com/multi.jpg?partNumber=1',
            headers: {
              host: 'cos.example.com',
              'x-cos-security-token': 'token-1',
            },
          },
          {
            partNumber: 2,
            uploadUrl: 'https://cos.example.com/multi.jpg?partNumber=2',
            headers: {
              'content-length': '4',
              'x-cos-security-token': 'token-2',
            },
          },
        ],
      },
    };
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(null, {
          status: 200,
          headers: {
            etag: '"etag-part-1"',
          },
        }),
      )
      .mockResolvedValueOnce(
        new Response(null, {
          status: 200,
          headers: {
            etag: '"etag-part-2"',
          },
        }),
      );
    const confirmUpload = vi.fn().mockResolvedValue({
      assetId: 'asset-multi',
      processingStage: 'queued',
    });

    const confirmation = await executeUploadFlow({
      ticket,
      file,
      contentSha256: 'sha-multi',
      fetchImpl,
      confirmUpload,
    });

    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(fetchImpl.mock.calls[0]?.[0]).toContain('partNumber=1');
    expect(fetchImpl.mock.calls[1]?.[0]).toContain('partNumber=2');

    const firstHeaders = fetchImpl.mock.calls[0]?.[1]?.headers as Headers;
    const secondHeaders = fetchImpl.mock.calls[1]?.[1]?.headers as Headers;
    expect(firstHeaders.get('x-cos-security-token')).toBe('token-1');
    expect(secondHeaders.get('x-cos-security-token')).toBe('token-2');
    expect(firstHeaders.has('Content-Type')).toBe(false);
    expect(secondHeaders.has('content-length')).toBe(false);

    const firstChunk = await (fetchImpl.mock.calls[0]?.[1]?.body as Blob).text();
    const secondChunk = await (fetchImpl.mock.calls[1]?.[1]?.body as Blob).text();
    expect(firstChunk).toBe('abcde');
    expect(secondChunk).toBe('fghi');

    expect(confirmUpload).toHaveBeenCalledWith({
      objectKey: 'imports/multi.jpg',
      contentLength: 9,
      contentSha256: 'sha-multi',
      etag: undefined,
      uploadId: 'upload-123',
      parts: [
        {
          partNumber: 1,
          etag: '"etag-part-1"',
        },
        {
          partNumber: 2,
          etag: '"etag-part-2"',
        },
      ],
    });
    expect(confirmation).toEqual({
      assetId: 'asset-multi',
      processingStage: 'queued',
    });
  });

  it('把对象存储网络失败转换成可操作的提示', () => {
    vi.stubGlobal('window', {
      location: {
        origin: 'http://192.168.8.80:3000',
      },
    });

    const error = describeObjectStorageFetchError(
      'http://localhost:9000/photonest-main/example.png',
      new TypeError('Failed to fetch'),
    );

    expect(error.message).toContain('浏览器无法访问 http://localhost:9000');
    expect(error.message).toContain('CORS 是否允许 http://192.168.8.80:3000');
  });
});
