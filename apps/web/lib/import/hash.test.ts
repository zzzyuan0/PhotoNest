import { afterEach, describe, expect, it, vi } from 'vitest';

import { computeSHA256 } from './hash';

describe('computeSHA256', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('优先使用 Web Crypto 计算摘要', async () => {
    const digest = new Uint8Array([0, 1, 171, 255]).buffer;
    const subtle = {
      digest: vi.fn().mockResolvedValue(digest),
    } as unknown as SubtleCrypto;

    vi.stubGlobal('crypto', { subtle } satisfies Partial<Crypto>);

    const result = await computeSHA256(
      new File(['hello'], 'hello.txt', { type: 'text/plain' }),
    );

    expect(subtle.digest).toHaveBeenCalledWith('SHA-256', expect.any(ArrayBuffer));
    expect(result).toBe('0001abff');
  });

  it('在 SubtleCrypto 不可用时回退到纯 JS 实现', async () => {
    vi.stubGlobal('crypto', {} satisfies Partial<Crypto>);

    const result = await computeSHA256(
      new File(['hello world'], 'hello.txt', { type: 'text/plain' }),
    );

    expect(result).toBe(
      'b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9',
    );
  });
});
