import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex } from '@noble/hashes/utils.js';

function hasSubtleCrypto(
  cryptoImpl: Crypto | undefined,
): cryptoImpl is Crypto & { subtle: SubtleCrypto } {
  return typeof cryptoImpl?.subtle?.digest === 'function';
}

export async function computeSHA256(file: Blob): Promise<string> {
  const buffer = await file.arrayBuffer();
  const cryptoImpl = globalThis.crypto;

  if (hasSubtleCrypto(cryptoImpl)) {
    const digest = await cryptoImpl.subtle.digest('SHA-256', buffer);
    return bytesToHex(new Uint8Array(digest));
  }

  // Fallback for insecure contexts or limited webviews where SubtleCrypto is absent.
  return bytesToHex(sha256(new Uint8Array(buffer)));
}
