<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';

import {
  apiFetch,
  type AssetDetailResponse,
  type AlbumDetailResponse,
  type AlbumsResponse,
  type AuthSessionResponse,
  type DuplicatesResponse,
  type DownloadGrant,
  type ExportJob,
  type ExportRequest,
  type PlacesResponse,
  type SearchResponse,
  type TimelineResponse,
} from '../lib/api/client';
import {
  buildAssetEmptyState,
  describeBackupStatus,
  describePreviewState,
  describeProcessingStage,
  pickNextSelectedAsset,
} from '../lib/ui/workflow';

type Session = AuthSessionResponse['session'];
type AssetSummary = TimelineResponse['items'][number];
type PlaceSummary = PlacesResponse['items'][number];
type DuplicateCandidate = DuplicatesResponse['items'][number];
type AlbumSummary = AlbumsResponse['items'][number];

const authSession = ref<Session | null>(null);
const libraryId = ref('');
const loading = ref(false);
const error = ref('');

const searchQuery = ref('');
const locationFilter = ref('');
const stageFilter = ref('');

const timeline = ref<AssetSummary[]>([]);
const searchResults = ref<AssetSummary[]>([]);
const places = ref<PlaceSummary[]>([]);
const duplicates = ref<DuplicateCandidate[]>([]);
const albums = ref<AlbumSummary[]>([]);

const newAlbumName = ref('');
const selectedAlbumId = ref('');
const selectedAlbum = ref<AlbumDetailResponse | null>(null);
const selectedAssetId = ref('');
const selectedAssetDetail = ref<AssetDetailResponse | null>(null);
const detailLoading = ref(false);
const detailRequestVersion = ref(0);
const previewUrlByAssetId = ref<Record<string, string>>({});
const previewErrorByAssetId = ref<Record<string, string>>({});
const previewLoadingAssetId = ref('');

const exportScope = ref<ExportRequest['scope']>('library');
const exportDateFrom = ref('');
const exportDateTo = ref('');
const exportJob = ref<ExportJob | null>(null);

const hasSession = computed(() => authSession.value !== null);
const hasLibrary = computed(() => libraryId.value.trim() !== '');
const canUseDashboard = computed(() => hasSession.value && hasLibrary.value);
const isSearching = computed(() => searchQuery.value.trim() !== '');
const hasFilters = computed(
  () => locationFilter.value.trim() !== '' || stageFilter.value.trim() !== '',
);
const displayItems = computed(() => (isSearching.value ? searchResults.value : timeline.value));
const selectedAlbumName = computed(() => selectedAlbum.value?.album.displayName ?? '未选择相册');
const selectedAssetSummary = computed(
  () => displayItems.value.find((item) => item.assetId === selectedAssetId.value) ?? null,
);
const selectedPreview = computed(() =>
  describePreviewState(
    selectedAssetDetail.value ?? selectedAssetSummary.value ?? { processingStage: 'accepted' },
  ),
);
const selectedAssetPreviewUrl = computed(
  () => previewUrlByAssetId.value[selectedAssetId.value] ?? '',
);
const selectedAssetPreviewError = computed(
  () => previewErrorByAssetId.value[selectedAssetId.value] ?? '',
);
const previewLoading = computed(
  () =>
    previewLoadingAssetId.value === selectedAssetId.value && selectedAssetPreviewUrl.value === '',
);
const emptyState = computed(() =>
  buildAssetEmptyState({
    hasSession: hasSession.value,
    hasLibrary: hasLibrary.value,
    isSearching: isSearching.value,
    hasFilters: hasFilters.value,
  }),
);
const canExport = computed(() => {
  if (!canUseDashboard.value) {
    return false;
  }
  if (exportScope.value === 'album') {
    return selectedAlbumId.value.trim() !== '';
  }
  if (exportScope.value === 'date-range') {
    return exportDateFrom.value.trim() !== '' || exportDateTo.value.trim() !== '';
  }
  return true;
});
const stageOptions = [
  { value: '', label: '全部状态' },
  { value: 'accepted', label: '已接收' },
  { value: 'stored', label: '已入库' },
  { value: 'derivatives-ready', label: '预览已准备' },
  { value: 'metadata-ready', label: '元数据整理中' },
  { value: 'ai-ready', label: 'AI 结果已就绪' },
  { value: 'indexed', label: '已可搜索' },
  { value: 'partial-failure', label: '需要人工留意' },
];

onMounted(async () => {
  await loadSession();
  if (canUseDashboard.value) {
    await refreshDashboard();
  }
});

watch(selectedAlbumId, async (albumId) => {
  if (!albumId) {
    selectedAlbum.value = null;
    return;
  }
  await openAlbum(albumId);
});

watch(
  () => displayItems.value.map((item) => item.assetId).join(','),
  async () => {
    const nextAssetId = pickNextSelectedAsset(displayItems.value, selectedAssetId.value);
    if (!nextAssetId) {
      clearAssetDetail();
      return;
    }

    if (nextAssetId !== selectedAssetId.value || !selectedAssetDetail.value) {
      await openAssetDetail(nextAssetId);
    }
  },
);

async function loadSession() {
  try {
    const response = await apiFetch<AuthSessionResponse>('/api/v1/auth/session');
    authSession.value = response.session;
    if (!libraryId.value && response.session.libraryIds?.length === 1) {
      libraryId.value = response.session.libraryIds[0];
    }
  } catch {
    authSession.value = null;
  }
}

async function refreshDashboard() {
  if (!canUseDashboard.value) {
    error.value = '请先登录并填写一个可访问的 library ID。';
    return;
  }

  loading.value = true;
  error.value = '';

  try {
    const [timelineResponse, placesResponse, duplicatesResponse, albumsResponse] =
      await Promise.all([
        apiFetch<TimelineResponse>(`/api/v1/discovery/timeline?${timelineParams()}`),
        apiFetch<PlacesResponse>(`/api/v1/discovery/places?${baseParams()}`),
        apiFetch<DuplicatesResponse>(`/api/v1/discovery/duplicates?${baseParams()}`),
        apiFetch<AlbumsResponse>(`/api/v1/albums?${baseParams()}`),
      ]);

    timeline.value = timelineResponse.items;
    places.value = placesResponse.items;
    duplicates.value = duplicatesResponse.items;
    albums.value = albumsResponse.items;

    if (selectedAlbumId.value) {
      await openAlbum(selectedAlbumId.value);
    } else {
      const favorites = albumsResponse.items.find((item) => item.kind === 'favorites');
      if (favorites) {
        selectedAlbumId.value = favorites.albumId;
      }
    }

    if (isSearching.value) {
      await runSearch();
    }
  } catch (caught) {
    error.value = formatError(caught);
  } finally {
    loading.value = false;
  }
}

async function refreshTimeline() {
  if (!canUseDashboard.value) {
    error.value = '请先确认登录状态和照片库。';
    return;
  }
  try {
    const response = await apiFetch<TimelineResponse>(
      `/api/v1/discovery/timeline?${timelineParams()}`,
    );
    timeline.value = response.items;
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function runSearch() {
  if (!canUseDashboard.value) {
    error.value = '请先确认登录状态和照片库。';
    return;
  }
  if (searchQuery.value.trim() === '') {
    searchResults.value = [];
    return;
  }

  try {
    const params = new URLSearchParams({
      libraryId: libraryId.value.trim(),
      query: searchQuery.value.trim(),
      limit: '30',
    });
    const response = await apiFetch<SearchResponse>(
      `/api/v1/discovery/search?${params.toString()}`,
    );
    searchResults.value = response.items;
  } catch (caught) {
    error.value = formatError(caught);
  }
}

function clearSearch() {
  searchQuery.value = '';
  searchResults.value = [];
}

async function clearFilters() {
  locationFilter.value = '';
  stageFilter.value = '';
  await refreshTimeline();
}

async function createAlbum() {
  if (!canUseDashboard.value || newAlbumName.value.trim() === '') {
    return;
  }

  try {
    const created = await apiFetch<AlbumSummary>('/api/v1/albums', {
      method: 'POST',
      body: {
        libraryId: libraryId.value.trim(),
        displayName: newAlbumName.value.trim(),
      },
    });
    newAlbumName.value = '';
    await refreshAlbums();
    selectedAlbumId.value = created.albumId;
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function refreshAlbums() {
  if (!canUseDashboard.value) {
    return;
  }
  const response = await apiFetch<AlbumsResponse>(`/api/v1/albums?${baseParams()}`);
  albums.value = response.items;
}

async function openAlbum(albumId: string) {
  if (!canUseDashboard.value || albumId.trim() === '') {
    return;
  }
  try {
    selectedAlbum.value = await apiFetch<AlbumDetailResponse>(
      `/api/v1/albums/${albumId}?${baseParams({ limit: '50' })}`,
    );
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function openAssetDetail(assetId: string) {
  if (!canUseDashboard.value || assetId.trim() === '') {
    return;
  }
  if (selectedAssetDetail.value?.assetId === assetId) {
    selectedAssetId.value = assetId;
    await ensureAssetPreview(selectedAssetDetail.value);
    return;
  }

  const requestVersion = detailRequestVersion.value + 1;
  detailRequestVersion.value = requestVersion;
  detailLoading.value = true;
  selectedAssetId.value = assetId;
  try {
    const detail = await apiFetch<AssetDetailResponse>(
      `/api/v1/assets/${assetId}?${baseParams()}`,
    );
    if (requestVersion !== detailRequestVersion.value) {
      return;
    }
    selectedAssetDetail.value = detail;
    await ensureAssetPreview(detail);
  } catch (caught) {
    if (requestVersion === detailRequestVersion.value) {
      error.value = formatError(caught);
    }
  } finally {
    if (requestVersion === detailRequestVersion.value) {
      detailLoading.value = false;
    }
  }
}

function clearAssetDetail() {
  detailRequestVersion.value += 1;
  selectedAssetId.value = '';
  selectedAssetDetail.value = null;
  previewLoadingAssetId.value = '';
}

async function ensureAssetPreview(detail: AssetDetailResponse) {
  if (!detail.mediaType.startsWith('image/')) {
    return;
  }
  if (previewUrlByAssetId.value[detail.assetId]) {
    return;
  }

  previewLoadingAssetId.value = detail.assetId;
  previewErrorByAssetId.value = {
    ...previewErrorByAssetId.value,
    [detail.assetId]: '',
  };

  try {
    const grant = await apiFetch<DownloadGrant>(
      `/api/v1/assets/${detail.assetId}/download?${baseParams()}`,
      { method: 'POST' },
    );
    if (grant.status !== 'ready' || !grant.url) {
      previewErrorByAssetId.value = {
        ...previewErrorByAssetId.value,
        [detail.assetId]: '当前图片还在准备受控预览地址，请稍后再试。',
      };
      return;
    }

    previewUrlByAssetId.value = {
      ...previewUrlByAssetId.value,
      [detail.assetId]: grant.url,
    };
  } catch (caught) {
    previewErrorByAssetId.value = {
      ...previewErrorByAssetId.value,
      [detail.assetId]: formatError(caught),
    };
  } finally {
    if (previewLoadingAssetId.value === detail.assetId) {
      previewLoadingAssetId.value = '';
    }
  }
}

async function favoriteAsset(assetId: string) {
  if (!canUseDashboard.value) {
    return;
  }
  try {
    await apiFetch(`/api/v1/assets/${assetId}/favorite`, {
      method: 'PUT',
      body: {
        libraryId: libraryId.value.trim(),
        favorite: true,
      },
    });
    await refreshAlbums();
    if (selectedAlbumId.value) {
      await openAlbum(selectedAlbumId.value);
    }
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function addAssetToSelectedAlbum(assetId: string) {
  if (!canUseDashboard.value || selectedAlbumId.value.trim() === '') {
    return;
  }
  try {
    await apiFetch(`/api/v1/albums/${selectedAlbumId.value}/assets`, {
      method: 'POST',
      body: {
        libraryId: libraryId.value.trim(),
        assetId,
      },
    });
    await refreshAlbums();
    await openAlbum(selectedAlbumId.value);
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function createExport() {
  if (!canExport.value) {
    return;
  }

  try {
    exportJob.value = await apiFetch<ExportJob>('/api/v1/exports', {
      method: 'POST',
      body: {
        libraryId: libraryId.value.trim(),
        scope: exportScope.value,
        albumId: exportScope.value === 'album' ? selectedAlbumId.value : undefined,
        dateFrom:
          exportScope.value === 'date-range' ? exportDateFrom.value || undefined : undefined,
        dateTo: exportScope.value === 'date-range' ? exportDateTo.value || undefined : undefined,
      },
    });
  } catch (caught) {
    error.value = formatError(caught);
  }
}

async function usePlace(label: string) {
  locationFilter.value = label;
  await refreshTimeline();
}

function baseParams(extra: Record<string, string> = {}) {
  const params = new URLSearchParams({
    libraryId: libraryId.value.trim(),
    ...extra,
  });
  return params.toString();
}

function timelineParams() {
  const params = new URLSearchParams({
    libraryId: libraryId.value.trim(),
    limit: '30',
  });
  if (locationFilter.value.trim()) {
    params.set('location', locationFilter.value.trim());
  }
  if (stageFilter.value.trim()) {
    params.set('stage', stageFilter.value.trim());
  }
  return params.toString();
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
  return '请求失败，请检查登录状态、libraryId 或后端服务配置。';
}
</script>

<template>
  <section class="hero-grid">
    <article class="surface-card overview-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">照片浏览工作台</p>
          <h2>一边浏览列表，一边在原位查看详情和整理动作</h2>
        </div>
        <div class="button-row">
          <NuxtLink class="button-secondary" to="/import">继续导入照片</NuxtLink>
          <NuxtLink class="button-ghost" to="/login">管理登录状态</NuxtLink>
          <button
            class="button-secondary"
            type="button"
            :disabled="loading"
            @click="refreshDashboard"
          >
            {{ loading ? '刷新中…' : '刷新照片库' }}
          </button>
        </div>
      </div>

      <p class="subtle-copy">
        主区域负责浏览、筛选和切换当前照片；右侧详情区固定展示当前选中资产，常用操作也都集中在附近。
      </p>

      <div class="metric-grid">
        <div class="metric-card">
          <span>已载入照片</span>
          <strong>{{ displayItems.length }}</strong>
        </div>
        <div class="metric-card">
          <span>地点聚合</span>
          <strong>{{ places.length }}</strong>
        </div>
        <div class="metric-card">
          <span>可管理相册</span>
          <strong>{{ albums.length }}</strong>
        </div>
        <div class="metric-card">
          <span>重复候选</span>
          <strong>{{ duplicates.length }}</strong>
        </div>
      </div>
    </article>

    <aside class="surface-card context-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">访问上下文</p>
          <h2>{{ hasSession ? '当前已登录' : '需要先登录' }}</h2>
        </div>
      </div>

      <div class="metric-grid">
        <div class="metric-card">
          <span>当前身份</span>
          <strong>{{ authSession?.displayName ?? '未检测到会话' }}</strong>
        </div>
        <div class="metric-card">
          <span>照片库</span>
          <strong>{{ hasLibrary ? libraryId : '尚未填写' }}</strong>
        </div>
      </div>

      <label class="field-group">
        <span class="field-label">Library ID</span>
        <input
          v-model="libraryId"
          class="text-input"
          type="text"
          placeholder="例如 11111111-1111-1111-1111-111111111111"
        />
      </label>

      <p class="status-copy">
        {{
          hasSession
            ? '确认 library ID 后就能刷新时间线和详情面板。'
            : '登录后会自动带回你可访问的照片库范围。'
        }}
      </p>
    </aside>
  </section>

  <section class="toolbar-grid">
    <article class="surface-card search-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">搜索与筛选</p>
          <h2>快速缩小当前浏览范围</h2>
        </div>
      </div>

      <div class="toolbar-form">
        <label class="field-group">
          <span class="field-label">搜索照片</span>
          <input
            v-model="searchQuery"
            class="text-input"
            type="text"
            placeholder="例如 beach sunset 或 location:guangzhou"
            @keydown.enter.prevent="runSearch"
          />
        </label>

        <label class="field-group">
          <span class="field-label">地点</span>
          <input
            v-model="locationFilter"
            class="text-input"
            type="text"
            placeholder="可直接点下方地点卡片回填"
          />
        </label>

        <label class="field-group">
          <span class="field-label">处理状态</span>
          <select v-model="stageFilter" class="select-input">
            <option
              v-for="option in stageOptions"
              :key="option.value || 'all'"
              :value="option.value"
            >
              {{ option.label }}
            </option>
          </select>
        </label>
      </div>

      <div class="button-row">
        <button
          class="button-primary"
          type="button"
          :disabled="!canUseDashboard"
          @click="runSearch"
        >
          搜索结果
        </button>
        <button
          class="button-secondary"
          type="button"
          :disabled="!canUseDashboard"
          @click="refreshTimeline"
        >
          应用筛选
        </button>
        <button class="button-ghost" type="button" @click="clearSearch">清空搜索</button>
        <button class="button-ghost" type="button" @click="clearFilters">清空筛选</button>
      </div>
    </article>

    <article class="surface-card place-card">
      <div class="section-heading">
        <div>
          <p class="mono-label">地点入口</p>
          <h2>按地点快速切换</h2>
        </div>
      </div>

      <div v-if="places.length" class="place-list">
        <button
          v-for="place in places"
          :key="place.label"
          class="place-chip"
          type="button"
          @click="usePlace(place.label)"
        >
          <strong>{{ place.label }}</strong>
          <span>{{ place.count }} 张</span>
          <small>{{ place.latestAt }}</small>
        </button>
      </div>
      <div v-else class="empty-state compact-empty">
        <h3>还没有地点聚合</h3>
        <p class="empty-copy">导入更多照片后，这里会成为快速切换浏览范围的入口。</p>
      </div>
    </article>
  </section>

  <p v-if="error" class="alert-banner error-line" data-tone="danger">{{ error }}</p>

  <section class="workspace-grid">
    <article class="surface-card list-panel">
      <div class="section-heading">
        <div>
          <p class="mono-label">{{ isSearching ? '搜索结果' : '照片列表' }}</p>
          <h2>{{ isSearching ? '当前搜索结果' : '按时间浏览当前照片库' }}</h2>
        </div>
        <span class="status-pill" :data-tone="displayItems.length ? 'success' : 'warning'">
          {{ displayItems.length }} 项
        </span>
      </div>

      <div v-if="displayItems.length > 0" class="asset-list">
        <article
          v-for="item in displayItems"
          :key="item.assetId"
          class="asset-card"
          :class="{ selected: selectedAssetId === item.assetId }"
        >
          <button class="asset-select" type="button" @click="openAssetDetail(item.assetId)">
            <div class="asset-preview" :data-tone="describePreviewState(item).tone">
              <strong>{{ describePreviewState(item).label }}</strong>
              <span>{{ describePreviewState(item).detail }}</span>
            </div>

            <div class="asset-header">
              <div>
                <p class="asset-id">{{ item.assetId }}</p>
                <strong>{{ item.mediaType }}</strong>
              </div>
              <span
                class="status-pill"
                :data-tone="
                  describePreviewState(item).tone === 'warning'
                    ? 'warning'
                    : describePreviewState(item).tone === 'ready'
                      ? 'success'
                      : 'warning'
                "
              >
                {{ describeProcessingStage(item.processingStage) }}
              </span>
            </div>

            <p class="asset-copy">
              {{ item.captionPreview || '这张照片已经入库，系统还在继续整理预览或补充说明。' }}
            </p>
            <div class="asset-meta">
              <span>时间：{{ item.timelineTimestamp }}</span>
              <span>备份：{{ describeBackupStatus(item.backupStatus) }}</span>
            </div>
          </button>

          <div class="button-row asset-actions">
            <button class="button-secondary" type="button" @click="favoriteAsset(item.assetId)">
              加入收藏
            </button>
            <button
              class="button-secondary"
              type="button"
              :disabled="selectedAlbumId === ''"
              @click="addAssetToSelectedAlbum(item.assetId)"
            >
              加入当前相册
            </button>
          </div>
        </article>
      </div>

      <div v-else class="empty-state">
        <h3>{{ emptyState.title }}</h3>
        <p class="empty-copy">{{ emptyState.detail }}</p>
        <div class="button-row">
          <NuxtLink v-if="!hasSession" class="button-primary" to="/login">
            {{ emptyState.primaryAction }}
          </NuxtLink>
          <button
            v-else-if="!hasLibrary"
            class="button-primary"
            type="button"
            @click="error = '请先在页面上方填写或确认 library ID，然后再刷新照片库。'"
          >
            {{ emptyState.primaryAction }}
          </button>
          <button
            v-else-if="isSearching || hasFilters"
            class="button-primary"
            type="button"
            @click="
              clearSearch();
              clearFilters();
            "
          >
            {{ emptyState.primaryAction }}
          </button>
          <NuxtLink v-else class="button-primary" to="/import">{{
            emptyState.primaryAction
          }}</NuxtLink>

          <NuxtLink v-if="!hasSession" class="button-secondary" to="/import">
            {{ emptyState.secondaryAction }}
          </NuxtLink>
          <NuxtLink v-else-if="!hasLibrary" class="button-secondary" to="/login">
            {{ emptyState.secondaryAction }}
          </NuxtLink>
          <NuxtLink v-else-if="isSearching || hasFilters" class="button-secondary" to="/import">
            {{ emptyState.secondaryAction }}
          </NuxtLink>
          <button v-else class="button-secondary" type="button" @click="refreshDashboard">
            {{ emptyState.secondaryAction }}
          </button>
        </div>
      </div>
    </article>

    <aside
      class="surface-card detail-panel"
      :class="{ active: detailLoading || selectedAssetDetail }"
    >
      <div class="section-heading">
        <div>
          <p class="mono-label">当前详情</p>
          <h2>{{ selectedAssetDetail ? '围绕当前照片继续操作' : '选择一张照片开始查看详情' }}</h2>
        </div>
        <button
          v-if="selectedAssetDetail"
          class="button-ghost"
          type="button"
          @click="clearAssetDetail"
        >
          清空
        </button>
      </div>

      <div v-if="selectedAssetDetail" class="detail-card">
        <div class="detail-preview-shell">
          <div v-if="selectedAssetPreviewUrl" class="detail-image-frame">
            <img
              class="detail-image"
              :src="selectedAssetPreviewUrl"
              :alt="selectedAssetDetail.assetId"
            />
          </div>
          <div v-else class="asset-preview detail-preview" :data-tone="selectedPreview.tone">
            <strong>{{ selectedPreview.label }}</strong>
            <span>{{ selectedPreview.detail }}</span>
          </div>

          <div v-if="previewLoading" class="detail-overlay">
            <p class="detail-loading">正在准备当前照片的预览…</p>
          </div>
        </div>

        <p v-if="selectedAssetPreviewError" class="status-copy">
          {{ selectedAssetPreviewError }}
        </p>

        <div class="metric-grid">
          <div class="metric-card">
            <span>资产 ID</span>
            <strong>{{ selectedAssetDetail.assetId }}</strong>
          </div>
          <div class="metric-card">
            <span>媒体类型</span>
            <strong>{{ selectedAssetDetail.mediaType }}</strong>
          </div>
          <div class="metric-card">
            <span>处理状态</span>
            <strong>{{ describeProcessingStage(selectedAssetDetail.processingStage) }}</strong>
          </div>
          <div class="metric-card">
            <span>备份状态</span>
            <strong>{{ describeBackupStatus(selectedAssetDetail.backupStatus) }}</strong>
          </div>
        </div>

        <p class="detail-copy">
          {{
            selectedAssetDetail.captionPreview ||
            '这张照片已经可以查看基础信息，更多摘要仍可能继续补齐。'
          }}
        </p>

        <div class="detail-meta">
          <span v-if="selectedAssetDetail.capturedAt"
            >拍摄时间：{{ selectedAssetDetail.capturedAt }}</span
          >
          <span v-else>拍摄时间暂未写入，后续整理完成后会继续补齐。</span>
          <span>
            {{
              selectedAssetDetail.thumbnailToken
                ? '缩略图句柄已就绪，可以继续使用当前详情区进行整理。'
                : '缩略图尚未就绪，但照片已经存在，不影响继续收藏或加入相册。'
            }}
          </span>
        </div>

        <div class="button-row">
          <button
            class="button-primary"
            type="button"
            @click="favoriteAsset(selectedAssetDetail.assetId)"
          >
            加入收藏
          </button>
          <button
            class="button-secondary"
            type="button"
            :disabled="selectedAlbumId === ''"
            @click="addAssetToSelectedAlbum(selectedAssetDetail.assetId)"
          >
            加入当前相册
          </button>
          <NuxtLink class="button-ghost" to="/import">继续导入更多照片</NuxtLink>
        </div>

        <div v-if="detailLoading" class="detail-inline-loading">
          <p class="detail-loading">正在读取当前照片的详情…</p>
        </div>
      </div>

      <div v-else-if="detailLoading" class="detail-inline-loading">
        <p class="detail-loading">正在读取当前照片的详情…</p>
      </div>

      <div v-else class="empty-state">
        <h3>先从左侧选一张照片</h3>
        <p class="empty-copy">
          详情区会固定在这里更新，不再要求你滚动到页面深处寻找当前资产的状态和操作。
        </p>
      </div>

      <section class="detail-subsection">
        <div class="section-heading">
          <div>
            <p class="mono-label">当前相册</p>
            <h3>{{ selectedAlbumName }}</h3>
          </div>
        </div>

        <div v-if="selectedAlbum?.items.length" class="album-asset-list">
          <article
            v-for="item in selectedAlbum.items"
            :key="`album-${item.assetId}`"
            class="album-asset-card"
          >
            <strong>{{ item.assetId }}</strong>
            <span>{{ describeProcessingStage(item.processingStage) }}</span>
            <small>{{ item.timelineTimestamp }}</small>
          </article>
        </div>
        <p v-else class="status-copy">当前相册还没有照片，可以从左侧列表直接加入。</p>
      </section>
    </aside>
  </section>

  <section class="support-grid">
    <article class="surface-card album-panel">
      <div class="section-heading">
        <div>
          <p class="mono-label">相册与收藏</p>
          <h2>把常用整理入口收在一起</h2>
        </div>
      </div>

      <div class="album-create">
        <input
          v-model="newAlbumName"
          class="text-input"
          type="text"
          placeholder="输入新的相册名称"
        />
        <button
          class="button-primary"
          type="button"
          :disabled="!canUseDashboard"
          @click="createAlbum"
        >
          创建相册
        </button>
      </div>

      <div v-if="albums.length" class="album-list">
        <button
          v-for="album in albums"
          :key="album.albumId"
          class="album-button"
          :class="{ selected: selectedAlbumId === album.albumId }"
          type="button"
          @click="selectedAlbumId = album.albumId"
        >
          <strong>{{ album.displayName }}</strong>
          <span>{{ album.kind }}</span>
          <small>{{ album.assetCount }} 项</small>
        </button>
      </div>
      <div v-else class="empty-state compact-empty">
        <h3>还没有相册数据</h3>
        <p class="empty-copy">创建一个精选相册后，就能把当前选中的照片直接加入进去。</p>
      </div>
    </article>

    <div class="secondary-stack">
      <details class="surface-card secondary-panel">
        <summary>
          <span>
            <strong>重复候选审查</strong>
            <small>{{ duplicates.length }} 组待查看</small>
          </span>
        </summary>
        <div v-if="duplicates.length" class="duplicate-list">
          <article
            v-for="item in duplicates"
            :key="`${item.primary.assetId}-${item.candidate.assetId}`"
            class="secondary-card"
          >
            <strong>{{ item.exact ? '高度重复' : '相似候选' }}</strong>
            <p>主资产：{{ item.primary.assetId }}</p>
            <p>候选资产：{{ item.candidate.assetId }}</p>
          </article>
        </div>
        <p v-else class="status-copy">当前没有需要人工审查的重复候选。</p>
      </details>

      <details class="surface-card secondary-panel">
        <summary>
          <span>
            <strong>导出</strong>
            <small>保留能力，但不干扰主浏览动线</small>
          </span>
        </summary>

        <div class="export-form">
          <label class="field-group">
            <span class="field-label">导出范围</span>
            <select v-model="exportScope" class="select-input">
              <option value="library">整库</option>
              <option value="album">当前相册</option>
              <option value="date-range">时间范围</option>
            </select>
          </label>

          <p v-if="exportScope === 'album'" class="status-copy">
            当前将导出：{{ selectedAlbumName }}
          </p>

          <div v-if="exportScope === 'date-range'" class="date-grid">
            <label class="field-group">
              <span class="field-label">开始日期</span>
              <input v-model="exportDateFrom" class="text-input" type="date" />
            </label>
            <label class="field-group">
              <span class="field-label">结束日期</span>
              <input v-model="exportDateTo" class="text-input" type="date" />
            </label>
          </div>

          <button class="button-primary" type="button" :disabled="!canExport" @click="createExport">
            生成导出包
          </button>

          <div v-if="exportJob" class="secondary-card">
            <p>状态：{{ exportJob.status }}</p>
            <p>资产数：{{ exportJob.assetCount }}</p>
            <p>到期时间：{{ exportJob.expiresAt }}</p>
            <a :href="exportJob.archiveUrl" target="_blank" rel="noreferrer">下载归档</a>
          </div>
        </div>
      </details>
    </div>
  </section>
</template>

<style scoped>
.hero-grid,
.toolbar-grid,
.workspace-grid,
.support-grid {
  display: grid;
  gap: 24px;
}

.hero-grid {
  grid-template-columns: minmax(0, 1.2fr) minmax(300px, 0.85fr);
}

.toolbar-grid {
  grid-template-columns: minmax(0, 1.25fr) minmax(280px, 0.8fr);
  margin-top: 24px;
}

.workspace-grid {
  grid-template-columns: minmax(0, 1.18fr) minmax(340px, 0.82fr);
  margin-top: 24px;
  align-items: start;
}

.support-grid {
  grid-template-columns: minmax(0, 1fr) minmax(320px, 0.95fr);
  margin-top: 24px;
}

.overview-card,
.context-card,
.search-card,
.place-card,
.list-panel,
.detail-panel,
.album-panel,
.secondary-panel {
  padding: 28px;
}

.overview-card,
.context-card,
.search-card,
.place-card,
.list-panel,
.detail-panel,
.album-panel,
.secondary-stack {
  display: grid;
  gap: 20px;
}

.overview-card h2,
.context-card h2,
.search-card h2,
.place-card h2,
.list-panel h2,
.detail-panel h2,
.album-panel h2 {
  margin: 0;
  font-size: clamp(1.5rem, 3vw, 2.2rem);
  line-height: 1.1;
}

.detail-panel {
  position: sticky;
  top: 20px;
}

.detail-panel.active {
  border-color: rgba(22, 57, 63, 0.2);
  box-shadow:
    0 24px 60px rgba(22, 44, 49, 0.08),
    0 0 0 3px rgba(22, 57, 63, 0.06);
}

.toolbar-form,
.asset-list,
.place-list,
.album-list,
.album-asset-list,
.duplicate-list,
.secondary-stack {
  display: grid;
  gap: 14px;
}

.asset-card,
.album-asset-card,
.secondary-card {
  padding: 16px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.7);
  border: 1px solid rgba(22, 57, 63, 0.1);
}

.asset-card.selected {
  border-color: rgba(22, 57, 63, 0.26);
  box-shadow: inset 0 0 0 1px rgba(22, 57, 63, 0.08);
}

.asset-select {
  width: 100%;
  display: grid;
  gap: 14px;
  padding: 0;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.asset-preview {
  display: grid;
  gap: 6px;
  padding: 14px 16px;
  border-radius: 18px;
  background: rgba(22, 57, 63, 0.07);
  color: #285c64;
}

.asset-preview[data-tone='ready'] {
  background: rgba(34, 92, 67, 0.1);
  color: #225c43;
}

.asset-preview[data-tone='warning'] {
  background: rgba(142, 91, 31, 0.12);
  color: #8e5b1f;
}

.asset-header,
.queue-main {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: start;
}

.asset-id,
.detail-loading {
  margin: 0;
  color: #5d6c71;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  font-size: 0.82rem;
}

.asset-copy,
.detail-copy,
.detail-meta,
.asset-meta {
  margin: 0;
  color: #5d6c71;
  line-height: 1.6;
}

.asset-meta,
.detail-meta {
  display: grid;
  gap: 8px;
}

.asset-actions {
  margin-top: 16px;
}

.detail-card,
.detail-subsection,
.export-form {
  display: grid;
  gap: 18px;
}

.detail-preview-shell {
  position: relative;
  display: grid;
  gap: 12px;
}

.detail-image-frame {
  overflow: hidden;
  border-radius: 22px;
  border: 1px solid rgba(22, 57, 63, 0.12);
  background:
    linear-gradient(135deg, rgba(22, 57, 63, 0.06), rgba(255, 255, 255, 0.82)),
    rgba(239, 244, 242, 0.9);
  min-height: 240px;
}

.detail-image {
  display: block;
  width: 100%;
  max-height: 420px;
  object-fit: contain;
  background: rgba(239, 244, 242, 0.86);
}

.detail-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 22px;
  background: rgba(248, 250, 249, 0.72);
  backdrop-filter: blur(4px);
}

.detail-inline-loading {
  padding: 14px 16px;
  border-radius: 18px;
  background: rgba(22, 57, 63, 0.05);
}

.place-chip,
.album-button {
  display: grid;
  gap: 6px;
  padding: 16px 18px;
  border-radius: 22px;
  text-align: left;
  border: 1px solid rgba(22, 57, 63, 0.1);
  background: rgba(255, 255, 255, 0.72);
  cursor: pointer;
}

.album-button.selected {
  border-color: rgba(22, 57, 63, 0.26);
  box-shadow: inset 0 0 0 1px rgba(22, 57, 63, 0.08);
}

.album-create {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
}

.secondary-panel summary {
  cursor: pointer;
  list-style: none;
}

.secondary-panel summary::-webkit-details-marker {
  display: none;
}

.secondary-panel summary span {
  display: grid;
  gap: 6px;
}

.secondary-panel small {
  color: #5d6c71;
}

.date-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.compact-empty {
  padding: 18px;
}

.error-line {
  margin-top: 24px;
}

@media (max-width: 1100px) {
  .hero-grid,
  .toolbar-grid,
  .workspace-grid,
  .support-grid,
  .date-grid {
    grid-template-columns: 1fr;
  }

  .detail-panel {
    position: static;
  }
}

@media (max-width: 720px) {
  .overview-card,
  .context-card,
  .search-card,
  .place-card,
  .list-panel,
  .detail-panel,
  .album-panel,
  .secondary-panel {
    padding: 22px;
  }

  .asset-header {
    flex-direction: column;
  }

  .album-create {
    grid-template-columns: 1fr;
  }
}
</style>
