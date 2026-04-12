<script setup lang="ts">
import type {
  AssetAcceptedResponse,
  AuthSessionResponse,
  ImportSession,
  UploadTicket,
} from '../lib/api/client';
import { apiFetch } from '../lib/api/client';

type UploadStatus =
  | 'pending'
  | 'hashing'
  | 'planning'
  | 'uploading'
  | 'confirming'
  | 'done'
  | 'error';

type UploadEntry = {
  id: string;
  file: File;
  sha256?: string;
  objectKey?: string;
  assetId?: string;
  processingStage?: string;
  status: UploadStatus;
  message: string;
};

const MULTIPART_THRESHOLD = 8 * 1024 * 1024;

const libraryId = ref('');
const authSession = ref<AuthSessionResponse['session'] | null>(null);
const importSession = ref<ImportSession | null>(null);
const entries = ref<UploadEntry[]>([]);
const formError = ref('');
const globalStatus = ref('等待选择文件');
const isSubmitting = ref(false);

const completedCount = computed(
  () => entries.value.filter((entry) => entry.status === 'done').length,
);
const canStart = computed(
  () => !isSubmitting.value && libraryId.value.trim() !== '' && entries.value.length > 0,
);

onMounted(async () => {
  try {
    const response = await apiFetch<AuthSessionResponse>('/api/v1/auth/session');
    authSession.value = response.session;
    if (!libraryId.value && response.session.libraryIds?.length === 1) {
      libraryId.value = response.session.libraryIds[0];
    }
  } catch {
    authSession.value = null;
  }
});

function onFilesSelected(event: Event) {
  const input = event.target as HTMLInputElement;
  const files = Array.from(input.files ?? []);

  entries.value = files.map((file, index) => ({
    id: `${file.name}-${file.size}-${index}`,
    file,
    status: 'pending',
    message: '等待上传',
  }));
  formError.value = '';
  globalStatus.value = files.length > 0 ? `已选择 ${files.length} 个文件` : '等待选择文件';
}

async function beginImport() {
  if (!canStart.value) {
    return;
  }

  formError.value = '';
  globalStatus.value = '正在创建导入会话';
  isSubmitting.value = true;

  try {
    importSession.value = await apiFetch<ImportSession>('/api/v1/import/sessions', {
      method: 'POST',
      body: {
        libraryId: libraryId.value.trim(),
        source: 'web-upload',
        expectedItemCount: entries.value.length,
        note: `Nuxt import started at ${new Date().toISOString()}`,
      },
    });

    globalStatus.value = `导入会话已创建：${importSession.value.id}`;
    for (const entry of entries.value) {
      await uploadEntry(entry);
    }

    globalStatus.value = `上传完成，成功接收 ${completedCount.value}/${entries.value.length} 个文件`;
  } catch (error) {
    formError.value = formatError(error);
    globalStatus.value = '导入会话创建失败';
  } finally {
    isSubmitting.value = false;
  }
}

async function uploadEntry(entry: UploadEntry) {
  if (!importSession.value) {
    throw new Error('导入会话尚未创建');
  }

  try {
    entry.status = 'hashing';
    entry.message = '计算 SHA-256';
    entry.sha256 = await computeSHA256(entry.file);

    entry.status = 'planning';
    entry.message = '请求上传票据';
    const ticket = await apiFetch<UploadTicket>(
      `/api/v1/import/sessions/${importSession.value.id}/uploads`,
      {
        method: 'POST',
        body: {
          libraryId: libraryId.value.trim(),
          fileName: entry.file.name,
          contentType: entry.file.type || 'application/octet-stream',
          contentLength: entry.file.size,
          contentSha256: entry.sha256,
          multipart: entry.file.size > MULTIPART_THRESHOLD,
        },
      },
    );

    let completedParts: Array<{ partNumber: number; etag: string }> | undefined;
    let uploadId: string | undefined;
    let uploadETag: string | undefined;

    if (ticket.multipart) {
      entry.status = 'uploading';
      entry.message = '分片直传对象存储';
      const multipartResult = await uploadMultipart(ticket, entry.file);
      completedParts = multipartResult.parts;
      uploadId = ticket.multipart.uploadId;
    } else {
      if (!ticket.url) {
        throw new Error('服务端没有返回可用的上传地址');
      }

      entry.status = 'uploading';
      entry.message = '浏览器直传对象存储';
      const uploadResponse = await fetch(ticket.url, {
        method: ticket.method || 'PUT',
        headers: buildUploadHeaders(ticket.headers, entry.file, true),
        body: entry.file,
        mode: 'cors',
      });
      if (!uploadResponse.ok) {
        throw new Error(`对象上传失败，状态码 ${uploadResponse.status}`);
      }

      uploadETag = uploadResponse.headers.get('etag') ?? undefined;
    }

    entry.objectKey = ticket.objectKey;
    entry.status = 'confirming';
    entry.message = '回调服务端确认上传';
    const confirmation = await apiFetch<AssetAcceptedResponse>(
      `/api/v1/import/sessions/${importSession.value.id}/confirm`,
      {
        method: 'POST',
        body: {
          libraryId: libraryId.value.trim(),
          objectKey: ticket.objectKey,
          contentLength: entry.file.size,
          contentSha256: entry.sha256,
          etag: uploadETag,
          uploadId,
          parts: completedParts,
        },
      },
    );

    entry.assetId = confirmation.assetId;
    entry.processingStage = confirmation.processingStage;
    entry.status = 'done';
    entry.message = `已确认入库，阶段：${confirmation.processingStage}`;
  } catch (error) {
    entry.status = 'error';
    entry.message = formatError(error);
  }
}

async function uploadMultipart(ticket: UploadTicket, file: File) {
  if (!ticket.multipart || ticket.multipart.parts.length === 0) {
    throw new Error('服务端没有返回有效的 multipart 票据');
  }

  const partSize = Math.ceil(file.size / ticket.multipart.parts.length);
  const parts: Array<{ partNumber: number; etag: string }> = [];

  for (const part of ticket.multipart.parts) {
    const start = (part.partNumber - 1) * partSize;
    const end = Math.min(file.size, start + partSize);
    const chunk = file.slice(start, end);
    const response = await fetch(part.uploadUrl, {
      method: 'PUT',
      headers: buildUploadHeaders(part.headers, file, false),
      body: chunk,
      mode: 'cors',
    });
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

function buildUploadHeaders(rawHeaders: Record<string, string> | undefined, file: File, includeContentType: boolean) {
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

async function computeSHA256(file: File) {
  const buffer = await file.arrayBuffer();
  const digest = await crypto.subtle.digest('SHA-256', buffer);
  return Array.from(new Uint8Array(digest))
    .map((chunk) => chunk.toString(16).padStart(2, '0'))
    .join('');
}

function formatError(error: unknown) {
  if (error && typeof error === 'object') {
    const maybeData = (error as { data?: { message?: string } }).data;
    if (maybeData?.message) {
      return maybeData.message;
    }
    const maybeMessage = (error as { message?: string }).message;
    if (maybeMessage) {
      return maybeMessage;
    }
  }

  return '发生未识别错误，请检查登录状态、libraryId 和对象存储配置';
}
</script>

<template>
  <section class="import-shell">
    <div class="hero">
      <div class="hero-copy">
        <p class="eyebrow">Direct Upload</p>
        <h1>先把导入闭环跑通，再把 AI 识别接在后面。</h1>
        <p class="summary">
          这个页面会创建导入会话、请求上传票据、让浏览器直传对象存储，并在上传完成后调用确认接口。
        </p>
        <p class="status-line">{{ globalStatus }}</p>
      </div>

      <aside class="hero-card">
        <p class="card-label">当前会话</p>
        <p v-if="authSession">
          已登录为 <strong>{{ authSession.displayName }}</strong>
        </p>
        <p v-else>还没有检测到登录会话，请先到登录页完成认证。</p>
        <p v-if="importSession" class="session-pill">
          importSessionId: <span>{{ importSession.id }}</span>
        </p>
      </aside>
    </div>

    <section class="panel">
      <div class="panel-head">
        <div>
          <p class="eyebrow">Upload Form</p>
          <h2>最小可用上传器</h2>
        </div>
        <NuxtLink class="ghost-link" to="/login">前往登录页</NuxtLink>
      </div>

      <label class="field">
        <span>目标 Library ID</span>
        <input
          v-model="libraryId"
          type="text"
          placeholder="例如 11111111-1111-1111-1111-111111111111"
        />
      </label>

      <label class="field">
        <span>选择照片文件</span>
        <input type="file" accept="image/*" multiple @change="onFilesSelected" />
      </label>

      <p v-if="formError" class="error">{{ formError }}</p>

      <div class="actions">
        <button class="primary" :disabled="!canStart" @click="beginImport">
          {{ isSubmitting ? '处理中...' : '开始导入' }}
        </button>
        <p class="hint">
          当前成功 {{ completedCount }}/{{ entries.length }} 个。上传接口会把 `libraryId`
          一并带回后端做权限边界校验。
        </p>
      </div>
    </section>

    <section class="panel list-panel">
      <div class="panel-head">
        <div>
          <p class="eyebrow">File Queue</p>
          <h2>文件状态</h2>
        </div>
      </div>

      <ul class="queue" v-if="entries.length > 0">
        <li v-for="entry in entries" :key="entry.id" :data-status="entry.status">
          <div>
            <p class="file-name">{{ entry.file.name }}</p>
            <p class="file-meta">
              {{ Math.max(1, Math.round(entry.file.size / 1024)) }} KB
              <span v-if="entry.assetId">· asset {{ entry.assetId }}</span>
              <span v-if="entry.objectKey">· {{ entry.objectKey }}</span>
            </p>
          </div>
          <div class="state">
            <strong>{{ entry.status }}</strong>
            <span>{{ entry.message }}</span>
          </div>
        </li>
      </ul>
      <p v-else class="empty">还没有选择任何文件。</p>
    </section>
  </section>
</template>

<style scoped>
.import-shell {
  display: grid;
  gap: 24px;
}

.hero {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(280px, 0.85fr);
  gap: 24px;
}

.hero-copy,
.hero-card,
.panel {
  border: 1px solid rgba(28, 45, 51, 0.08);
  border-radius: 28px;
  background:
    radial-gradient(circle at top left, rgba(212, 233, 237, 0.75), transparent 42%),
    linear-gradient(140deg, rgba(255, 255, 255, 0.92), rgba(249, 243, 233, 0.88));
  box-shadow: 0 24px 48px rgba(26, 44, 49, 0.08);
}

.hero-copy {
  padding: 36px;
}

.hero-card,
.panel {
  padding: 28px;
}

.eyebrow,
.card-label {
  margin: 0 0 12px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #617477;
  font-size: 0.82rem;
}

h1,
h2 {
  margin: 0;
  color: #16373f;
}

h1 {
  font-size: clamp(2.1rem, 4vw, 4.2rem);
  line-height: 0.97;
  max-width: 12ch;
}

h2 {
  font-size: 1.45rem;
}

.summary,
.status-line,
.hint,
.empty,
.file-meta,
.hero-card p {
  color: #4b5f62;
  line-height: 1.7;
}

.status-line {
  margin-top: 20px;
  font-weight: 600;
}

.session-pill {
  margin-top: 18px;
  padding: 12px 14px;
  border-radius: 18px;
  background: rgba(23, 58, 65, 0.08);
  word-break: break-all;
}

.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  margin-bottom: 20px;
}

.ghost-link {
  border-radius: 999px;
  padding: 10px 14px;
  border: 1px solid rgba(22, 55, 63, 0.14);
  color: #16373f;
  font-weight: 600;
}

.field {
  display: grid;
  gap: 10px;
  margin-bottom: 18px;
}

.field span {
  font-weight: 600;
  color: #16373f;
}

.field input {
  border-radius: 18px;
  border: 1px solid rgba(22, 55, 63, 0.14);
  background: rgba(255, 255, 255, 0.92);
  padding: 14px 16px;
  color: #16373f;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  align-items: center;
}

.primary {
  border: none;
  border-radius: 999px;
  padding: 12px 20px;
  background: #16373f;
  color: #f7f2e8;
  font-weight: 700;
  cursor: pointer;
}

.primary:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.error {
  margin: 0 0 16px;
  color: #9a3f2e;
  font-weight: 600;
}

.queue {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 14px;
}

.queue li {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(200px, 0.8fr);
  gap: 16px;
  align-items: start;
  border-radius: 22px;
  padding: 18px 20px;
  border: 1px solid rgba(22, 55, 63, 0.08);
  background: rgba(255, 255, 255, 0.8);
}

.queue li[data-status='done'] {
  background: linear-gradient(135deg, rgba(221, 240, 228, 0.95), rgba(252, 248, 237, 0.94));
}

.queue li[data-status='error'] {
  background: linear-gradient(135deg, rgba(248, 226, 220, 0.95), rgba(255, 248, 242, 0.96));
}

.file-name {
  margin: 0;
  font-weight: 700;
  color: #16373f;
}

.file-meta {
  margin: 6px 0 0;
  word-break: break-all;
}

.state {
  display: grid;
  gap: 6px;
}

.state strong {
  text-transform: capitalize;
  color: #16373f;
}

@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
  }

  .queue li {
    grid-template-columns: 1fr;
  }
}
</style>
