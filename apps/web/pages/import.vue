<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import type { AuthSessionResponse, ImportSession, UploadTicket } from '../lib/api/client';
import { apiFetch } from '../lib/api/client';
import { computeSHA256 } from '../lib/import/hash';
import { executeUploadFlow } from '../lib/import/upload';
import {
  describeProcessingStage,
  describeUploadStatus,
  summarizeUploadQueue,
  type UploadStatus,
} from '../lib/ui/workflow';

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
const globalStatus = ref('先确认登录状态，再选择照片开始导入。');
const isSubmitting = ref(false);

const queueSummary = computed(() => summarizeUploadQueue(entries.value));
const completedCount = computed(() => queueSummary.value.done);
const failedCount = computed(() => queueSummary.value.failed);
const activeCount = computed(() => queueSummary.value.active);
const hasSession = computed(() => authSession.value !== null);
const hasLibrary = computed(() => libraryId.value.trim() !== '');
const hasFiles = computed(() => entries.value.length > 0);
const canStart = computed(
  () => !isSubmitting.value && hasSession.value && hasLibrary.value && hasFiles.value,
);
const availableLibraries = computed(() => authSession.value?.libraryIds ?? []);
const queueProgressLabel = computed(() => {
  if (!entries.value.length) {
    return '还没有加入任何文件';
  }

  if (isSubmitting.value) {
    return `正在导入 ${queueSummary.value.total} 个文件，已完成 ${queueSummary.value.done} 个`;
  }

  if (queueSummary.value.done > 0 || queueSummary.value.failed > 0) {
    return `本轮处理完成 ${queueSummary.value.done} 个，需留意 ${queueSummary.value.failed} 个`;
  }

  return `已加入 ${queueSummary.value.total} 个文件，等待开始导入`;
});
const setupSteps = computed(() => [
  {
    title: '确认会话',
    detail: hasSession.value
      ? `当前登录：${authSession.value?.displayName}`
      : '先登录后才能创建导入会话。',
    ready: hasSession.value,
  },
  {
    title: '选择照片库',
    detail: hasLibrary.value
      ? `目标照片库：${libraryId.value}`
      : '填写或选择一个可访问的 library ID。',
    ready: hasLibrary.value,
  },
  {
    title: '加入文件',
    detail: hasFiles.value
      ? `当前已选择 ${entries.value.length} 个文件。`
      : '从本地选择要上传的照片文件。',
    ready: hasFiles.value,
  },
  {
    title: '开始导入',
    detail: canStart.value ? '条件已满足，现在可以开始导入。' : '满足前三步后才能开始上传。',
    ready: canStart.value,
  },
]);
const resultTone = computed(() => {
  if (failedCount.value > 0) {
    return 'warning';
  }
  if (completedCount.value > 0) {
    return 'success';
  }
  return 'default';
});

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
    message: '等待开始导入',
  }));
  formError.value = '';
  globalStatus.value =
    files.length > 0
      ? `已加入 ${files.length} 个文件，准备创建导入会话。`
      : '先确认登录状态，再选择照片开始导入。';
}

async function beginImport() {
  if (!hasSession.value) {
    formError.value = '请先前往登录页完成认证，再回来开始导入。';
    return;
  }
  if (!hasLibrary.value) {
    formError.value = '请先填写一个可访问的 library ID。';
    return;
  }
  if (!hasFiles.value) {
    formError.value = '请先选择至少一个照片文件。';
    return;
  }
  if (!canStart.value) {
    return;
  }

  formError.value = '';
  globalStatus.value = '正在创建导入会话…';
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

    globalStatus.value = '导入会话已创建，正在依次处理文件。';
    for (const entry of entries.value) {
      await uploadEntry(entry);
    }

    if (failedCount.value > 0) {
      globalStatus.value = `本轮导入已结束，成功 ${completedCount.value} 个，需留意 ${failedCount.value} 个。`;
    } else {
      globalStatus.value = `导入完成，${completedCount.value} 个文件已进入照片库。`;
    }
  } catch (error) {
    formError.value = formatError(error);
    globalStatus.value = '导入会话创建失败，请检查登录状态与照片库配置。';
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
    entry.message = '正在计算文件校验值';
    entry.sha256 = await computeSHA256(entry.file);

    entry.status = 'planning';
    entry.message = '正在申请上传票据';
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

    entry.objectKey = ticket.objectKey;
    entry.status = 'uploading';
    entry.message = ticket.multipart ? '正在分片上传文件' : '正在上传文件';
    const confirmation = await executeUploadFlow({
      ticket,
      file: entry.file,
      contentSha256: entry.sha256,
      fetchImpl: (input, init) =>
        fetch(input, {
          ...init,
          mode: 'cors',
        }),
      confirmUpload: async (payload) => {
        entry.status = 'confirming';
        entry.message = '正在确认导入结果';
        return await apiFetch(`/api/v1/import/sessions/${importSession.value!.id}/confirm`, {
          method: 'POST',
          body: {
            libraryId: libraryId.value.trim(),
            ...payload,
          },
        });
      },
    });

    entry.assetId = confirmation.assetId;
    entry.processingStage = confirmation.processingStage;
    entry.status = 'done';
    entry.message = `已进入照片库，当前阶段：${describeProcessingStage(confirmation.processingStage)}`;
  } catch (error) {
    entry.status = 'error';
    entry.message = formatError(error);
  }
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

  return '发生未识别错误，请检查登录状态、libraryId 和对象存储配置。';
}
</script>

<template>
  <section class="import-layout">
    <article class="surface-card hero-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">导入步骤</p>
          <h2>把上传流程拆成看得懂的四步</h2>
        </div>
        <span class="status-pill" :data-tone="resultTone === 'default' ? 'warning' : resultTone">
          {{ queueProgressLabel }}
        </span>
      </div>

      <p class="subtle-copy">
        这里先确认登录与目标照片库，再选择文件、启动上传，并在完成后给你明确的下一步入口。
      </p>

      <div class="step-grid">
        <article
          v-for="(step, index) in setupSteps"
          :key="step.title"
          class="step-card"
          :data-ready="step.ready"
        >
          <span class="step-index">0{{ index + 1 }}</span>
          <strong>{{ step.title }}</strong>
          <p>{{ step.detail }}</p>
        </article>
      </div>
    </article>

    <aside class="surface-card session-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">准备条件</p>
          <h2>{{ hasSession ? '可以开始导入' : '请先登录' }}</h2>
        </div>
      </div>

      <div class="metric-grid">
        <div class="metric-card">
          <span>登录状态</span>
          <strong>{{ hasSession ? authSession?.displayName : '未检测到会话' }}</strong>
        </div>
        <div class="metric-card">
          <span>目标照片库</span>
          <strong>{{ hasLibrary ? libraryId : '尚未选择' }}</strong>
        </div>
        <div class="metric-card">
          <span>导入会话</span>
          <strong>{{ importSession?.id ?? '尚未创建' }}</strong>
        </div>
      </div>

      <div class="button-row">
        <NuxtLink class="button-secondary" to="/login">去登录页</NuxtLink>
        <NuxtLink class="button-ghost" to="/">查看照片库</NuxtLink>
      </div>
    </aside>
  </section>

  <section class="import-grid">
    <article class="surface-card upload-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">文件选择</p>
          <h2>确认照片库并加入文件</h2>
        </div>
      </div>

      <label class="field-group">
        <span class="field-label">目标 Library ID</span>
        <input
          v-model="libraryId"
          class="text-input"
          type="text"
          placeholder="例如 11111111-1111-1111-1111-111111111111"
          list="library-options"
        />
      </label>
      <datalist id="library-options">
        <option v-for="id in availableLibraries" :key="id" :value="id" />
      </datalist>

      <label class="field-group">
        <span class="field-label">选择照片文件</span>
        <input
          class="text-input file-input"
          type="file"
          accept="image/*"
          multiple
          @change="onFilesSelected"
        />
      </label>

      <p class="status-copy">{{ globalStatus }}</p>
      <p v-if="formError" class="alert-banner" data-tone="danger">{{ formError }}</p>

      <div class="button-row">
        <button class="button-primary" :disabled="!canStart" @click="beginImport">
          {{ isSubmitting ? '正在导入…' : '开始导入照片' }}
        </button>
        <NuxtLink class="button-secondary" to="/">导入后查看照片</NuxtLink>
      </div>
    </article>

    <article class="surface-card progress-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">上传进度</p>
          <h2>整体反馈</h2>
        </div>
      </div>

      <div class="metric-grid">
        <div class="metric-card">
          <span>总文件数</span>
          <strong>{{ queueSummary.total }}</strong>
        </div>
        <div class="metric-card">
          <span>已完成</span>
          <strong>{{ completedCount }}</strong>
        </div>
        <div class="metric-card">
          <span>处理中</span>
          <strong>{{ activeCount }}</strong>
        </div>
        <div class="metric-card">
          <span>需留意</span>
          <strong>{{ failedCount }}</strong>
        </div>
      </div>

      <div
        v-if="completedCount > 0 || failedCount > 0"
        class="result-card"
        :data-tone="failedCount > 0 ? 'warning' : 'success'"
      >
        <strong>
          {{
            failedCount > 0
              ? '本轮导入已经结束，部分文件需要留意。'
              : '本轮导入已经完成，可以继续查看照片。'
          }}
        </strong>
        <p>
          {{
            failedCount > 0
              ? `成功 ${completedCount} 个，需处理 ${failedCount} 个。你可以先去照片库查看已成功的结果，再决定是否继续导入。`
              : `成功导入 ${completedCount} 个文件，现在可以前往照片库查看详情，或者继续选择下一批文件。`
          }}
        </p>
        <div class="button-row">
          <NuxtLink class="button-primary" to="/">前往照片库</NuxtLink>
          <button
            class="button-secondary"
            type="button"
            @click="globalStatus = '可以继续选择下一批文件。'"
          >
            继续导入
          </button>
        </div>
      </div>
    </article>
  </section>

  <section class="surface-card queue-card">
    <div class="section-heading">
      <div>
        <p class="mono-label">文件队列</p>
        <h2>逐项查看每个文件的当前状态</h2>
      </div>
    </div>

    <div v-if="entries.length > 0" class="queue-list">
      <article
        v-for="entry in entries"
        :key="entry.id"
        class="queue-item"
        :data-tone="describeUploadStatus(entry.status).tone"
      >
        <div class="queue-main">
          <div>
            <p class="queue-name">{{ entry.file.name }}</p>
            <p class="queue-meta">
              {{ Math.max(1, Math.round(entry.file.size / 1024)) }} KB
              <span v-if="entry.assetId">· 资产 {{ entry.assetId }}</span>
              <span v-if="entry.processingStage">
                · {{ describeProcessingStage(entry.processingStage) }}
              </span>
            </p>
          </div>
          <span
            class="status-pill"
            :data-tone="
              describeUploadStatus(entry.status).tone === 'danger'
                ? 'danger'
                : describeUploadStatus(entry.status).tone === 'success'
                  ? 'success'
                  : 'warning'
            "
          >
            {{ describeUploadStatus(entry.status).step }}
          </span>
        </div>
        <p class="status-copy">{{ describeUploadStatus(entry.status).detail }}</p>
        <p class="queue-message">{{ entry.message }}</p>
      </article>
    </div>

    <div v-else class="empty-state">
      <h3>先选择一批照片</h3>
      <p class="empty-copy">
        导入页会按“准备条件 -> 选择文件 -> 上传进度 -> 完成反馈”的顺序引导你完成整条流程。
      </p>
      <div class="button-row">
        <NuxtLink class="button-secondary" to="/login">先去登录</NuxtLink>
        <NuxtLink class="button-ghost" to="/">查看现有照片</NuxtLink>
      </div>
    </div>
  </section>
</template>

<style scoped>
.import-layout,
.import-grid {
  display: grid;
  gap: 24px;
}

.import-layout {
  grid-template-columns: minmax(0, 1.3fr) minmax(300px, 0.9fr);
}

.import-grid {
  grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
  margin-top: 24px;
}

.hero-card,
.session-card,
.upload-card,
.progress-card,
.queue-card {
  padding: 28px;
}

.hero-card,
.session-card,
.upload-card,
.progress-card,
.queue-card {
  display: grid;
  gap: 20px;
}

.hero-card h2,
.session-card h2,
.upload-card h2,
.progress-card h2,
.queue-card h2 {
  margin: 0;
  font-size: clamp(1.5rem, 3vw, 2.3rem);
  line-height: 1.1;
}

.step-grid,
.queue-list {
  display: grid;
  gap: 14px;
}

.step-grid {
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.step-card,
.queue-item,
.result-card {
  padding: 18px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid rgba(22, 57, 63, 0.1);
}

.step-card[data-ready='true'] {
  border-color: rgba(34, 92, 67, 0.16);
  background: rgba(245, 252, 248, 0.82);
}

.step-card strong,
.result-card strong {
  font-size: 1rem;
}

.step-card p,
.result-card p,
.queue-message,
.queue-meta {
  margin: 8px 0 0;
  color: #5d6c71;
  line-height: 1.55;
}

.step-index {
  display: inline-flex;
  margin-bottom: 12px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  color: #5d6c71;
}

.file-input {
  padding-top: 10px;
  padding-bottom: 10px;
}

.result-card[data-tone='success'] {
  background: rgba(245, 252, 248, 0.86);
}

.result-card[data-tone='warning'] {
  background: rgba(255, 248, 239, 0.92);
}

.queue-main {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: start;
}

.queue-name {
  margin: 0;
  font-weight: 600;
  color: #183036;
}

.queue-meta {
  font-size: 0.92rem;
}

@media (max-width: 980px) {
  .import-layout,
  .import-grid {
    grid-template-columns: 1fr;
  }

  .hero-card,
  .session-card,
  .upload-card,
  .progress-card,
  .queue-card {
    padding: 22px;
  }
}

@media (max-width: 640px) {
  .queue-main {
    flex-direction: column;
  }
}
</style>
