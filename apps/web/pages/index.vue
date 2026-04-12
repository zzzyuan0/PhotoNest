<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';

import {
  apiFetch,
  type AssetDetailResponse,
  type AlbumDetailResponse,
  type AlbumsResponse,
  type AuthSessionResponse,
  type DuplicatesResponse,
  type ExportJob,
  type ExportRequest,
  type PlacesResponse,
  type SearchResponse,
  type TimelineResponse,
} from '../lib/api/client';

type Session = AuthSessionResponse['session'];
type AssetSummary = TimelineResponse['items'][number];
type PlaceSummary = PlacesResponse['items'][number];
type DuplicateCandidate = DuplicatesResponse['items'][number];
type AlbumSummary = AlbumsResponse['items'][number];
type AssetDetail = AssetDetailResponse;

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
const selectedAssetDetail = ref<AssetDetail | null>(null);
const detailLoading = ref(false);

const exportScope = ref<ExportRequest['scope']>('library');
const exportDateFrom = ref('');
const exportDateTo = ref('');
const exportJob = ref<ExportJob | null>(null);

const hasSession = computed(() => authSession.value !== null);
const displayItems = computed(() =>
  searchQuery.value.trim() !== '' ? searchResults.value : timeline.value,
);
const selectedAlbumName = computed(() => selectedAlbum.value?.album.displayName ?? '未选择相册');
const canUseDashboard = computed(() => hasSession.value && libraryId.value.trim() !== '');
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
    error.value = '请先登录并提供可访问的 libraryId。';
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

    if (searchQuery.value.trim() !== '') {
      await runSearch();
    }
    if (selectedAssetId.value.trim() !== '') {
      await openAssetDetail(selectedAssetId.value);
    }
  } catch (caught) {
    error.value = formatError(caught);
  } finally {
    loading.value = false;
  }
}

async function refreshTimeline() {
  if (!canUseDashboard.value) {
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

  detailLoading.value = true;
  selectedAssetId.value = assetId;
  try {
    selectedAssetDetail.value = await apiFetch<AssetDetailResponse>(
      `/api/v1/assets/${assetId}?${baseParams()}`,
    );
  } catch (caught) {
    error.value = formatError(caught);
  } finally {
    detailLoading.value = false;
  }
}

function clearAssetDetail() {
  selectedAssetId.value = '';
  selectedAssetDetail.value = null;
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

function clearSearch() {
  searchQuery.value = '';
  searchResults.value = [];
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

function formatStage(value: string) {
  return value.replaceAll('-', ' ');
}

function describeStage(value: string) {
  switch (value) {
    case 'accepted':
      return '已接收';
    case 'stored':
      return '已入库';
    case 'derivatives-ready':
      return '衍生资源已生成';
    case 'metadata-ready':
      return '元数据处理中';
    case 'ai-ready':
      return 'AI 结果已就绪';
    case 'indexed':
      return '已建立索引';
    case 'partial-failure':
      return '部分处理失败';
    default:
      return formatStage(value);
  }
}

function previewStatusLine(item: { processingStage: string; thumbnailToken?: string }) {
  if (item.thumbnailToken) {
    return '缩略图令牌已就绪，可继续通过受控句柄读取。';
  }
  if (item.processingStage === 'partial-failure') {
    return '缩略图仍未就绪，当前保留受控占位，避免误判为上传失败。';
  }
  return '缩略图仍在后台准备，这张资产已经入库并会继续显示处理中状态。';
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
  <section class="hero">
    <div class="hero-copy">
      <p class="eyebrow">Photo Discovery Desk</p>
      <h1>把时间线、地点、重复审查和精选整理拉到一个工作台里。</h1>
      <p class="summary">
        这里直接连真实 API：支持按地点过滤时间线、查看重复候选、把资产加入收藏或精选相册，
        也可以从当前库或当前相册生成短时效导出包。
      </p>
      <div class="hero-actions">
        <NuxtLink class="primary" to="/import">继续导入照片</NuxtLink>
        <NuxtLink class="secondary" to="/login">管理登录会话</NuxtLink>
        <button class="secondary" type="button" :disabled="loading" @click="refreshDashboard">
          {{ loading ? '刷新中...' : '刷新工作台' }}
        </button>
      </div>
    </div>

    <aside class="hero-card">
      <p class="card-label">访问上下文</p>
      <p v-if="authSession">
        已登录为 <strong>{{ authSession.displayName }}</strong>
      </p>
      <p v-else>当前未检测到会话，请先前往登录页完成认证。</p>

      <label class="field">
        <span>Library ID</span>
        <input
          v-model="libraryId"
          type="text"
          placeholder="例如 11111111-1111-1111-1111-111111111111"
        />
      </label>

      <p class="meta-line">
        已载入 {{ timeline.length }} 条时间线摘要，{{ albums.length }} 个可管理相册。
      </p>
    </aside>
  </section>

  <section class="toolbar">
    <div class="search-panel">
      <label class="field">
        <span>混合搜索</span>
        <input
          v-model="searchQuery"
          type="text"
          placeholder="例如 beach sunset 或 location:guangzhou"
          @keydown.enter.prevent="runSearch"
        />
      </label>
      <div class="inline-actions">
        <button class="primary" type="button" :disabled="!canUseDashboard" @click="runSearch">
          搜索
        </button>
        <button class="secondary" type="button" @click="clearSearch">清空</button>
      </div>
    </div>

    <div class="filter-panel">
      <label class="field compact">
        <span>地点过滤</span>
        <input v-model="locationFilter" type="text" placeholder="点击地点卡片可自动填充" />
      </label>
      <label class="field compact">
        <span>阶段过滤</span>
        <select v-model="stageFilter">
          <option value="">全部阶段</option>
          <option value="derivatives-ready">derivatives-ready</option>
          <option value="metadata-ready">metadata-ready</option>
          <option value="ai-ready">ai-ready</option>
          <option value="indexed">indexed</option>
          <option value="partial-failure">partial-failure</option>
        </select>
      </label>
      <button class="secondary" type="button" :disabled="!canUseDashboard" @click="refreshTimeline">
        应用时间线过滤
      </button>
    </div>
  </section>

  <p v-if="error" class="error-banner">{{ error }}</p>

  <section class="dashboard">
    <div class="main-column">
      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">{{ searchQuery ? 'Search Result' : 'Timeline' }}</p>
            <h2>{{ searchQuery ? '混合搜索结果' : '按时间浏览照片库' }}</h2>
          </div>
          <p class="count">{{ displayItems.length }} 项</p>
        </div>

        <div v-if="displayItems.length > 0" class="asset-list">
          <article v-for="item in displayItems" :key="item.assetId" class="asset-card">
            <div class="asset-preview" :data-ready="Boolean(item.thumbnailToken)">
              <strong>{{ item.thumbnailToken ? '缩略图已就绪' : '处理中占位' }}</strong>
              <span>{{ previewStatusLine(item) }}</span>
            </div>
            <div class="asset-top">
              <div>
                <p class="asset-id">{{ item.assetId }}</p>
                <strong>{{ item.mediaType }}</strong>
              </div>
              <span class="chip">{{ describeStage(item.processingStage) }}</span>
            </div>
            <p class="asset-time">{{ item.timelineTimestamp }}</p>
            <p class="asset-copy">
              {{ item.captionPreview || '这张资产还没有可公开预览的 caption，或仍在后台处理中。' }}
            </p>
            <div class="asset-meta">
              <span>备份：{{ item.backupStatus }}</span>
              <span>{{ item.thumbnailToken ? '缩略图令牌已生成' : '缩略图待就绪' }}</span>
            </div>
            <div class="asset-actions">
              <button class="secondary" type="button" @click="openAssetDetail(item.assetId)">
                查看详情
              </button>
              <button class="secondary" type="button" @click="favoriteAsset(item.assetId)">
                加入收藏
              </button>
              <button
                class="secondary"
                type="button"
                :disabled="selectedAlbumId === ''"
                @click="addAssetToSelectedAlbum(item.assetId)"
              >
                加入当前相册
              </button>
            </div>
          </article>
        </div>
        <p v-else class="empty">当前还没有可展示的结果，可以先导入照片或调整搜索条件。</p>
      </article>

      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">Duplicate Review</p>
            <h2>重复候选审查</h2>
          </div>
          <p class="count">{{ duplicates.length }} 组</p>
        </div>
        <div v-if="duplicates.length > 0" class="duplicate-grid">
          <article
            v-for="item in duplicates"
            :key="`${item.primary.assetId}-${item.candidate.assetId}`"
            class="pair-card"
          >
            <div class="pair-head">
              <span>主资产</span>
              <span>{{ item.exact ? 'exact' : 'similar' }}</span>
            </div>
            <p>{{ item.primary.assetId }}</p>
            <small>{{ item.primary.timelineTimestamp }}</small>
            <div class="pair-divider"></div>
            <div class="pair-head">
              <span>候选资产</span>
              <button class="ghost" type="button" @click="favoriteAsset(item.primary.assetId)">
                收藏主图
              </button>
            </div>
            <p>{{ item.candidate.assetId }}</p>
            <small>{{ item.candidate.timelineTimestamp }}</small>
          </article>
        </div>
        <p v-else class="empty">当前没有发现需要人工审查的重复候选。</p>
      </article>
    </div>

    <div class="side-column">
      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">Places</p>
            <h2>地点浏览</h2>
          </div>
        </div>
        <div v-if="places.length > 0" class="place-list">
          <button
            v-for="place in places"
            :key="place.label"
            class="place-card"
            type="button"
            @click="usePlace(place.label)"
          >
            <strong>{{ place.label }}</strong>
            <span>{{ place.count }} 张</span>
            <small>{{ place.latestAt }}</small>
          </button>
        </div>
        <p v-else class="empty">当前库中还没有可公开显示的地点聚合。</p>
      </article>

      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">Albums</p>
            <h2>收藏与精选</h2>
          </div>
        </div>

        <div class="album-create">
          <input v-model="newAlbumName" type="text" placeholder="输入新的精选相册名称" />
          <button class="primary" type="button" :disabled="!canUseDashboard" @click="createAlbum">
            创建
          </button>
        </div>

        <div v-if="albums.length > 0" class="album-list">
          <button
            v-for="album in albums"
            :key="album.albumId"
            class="album-card"
            :class="{ selected: selectedAlbumId === album.albumId }"
            type="button"
            @click="selectedAlbumId = album.albumId"
          >
            <strong>{{ album.displayName }}</strong>
            <span>{{ album.kind }}</span>
            <small>{{ album.assetCount }} 项</small>
          </button>
        </div>
        <p v-else class="empty">还没有相册数据，创建一个精选相册试试看。</p>
      </article>

      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">Export</p>
            <h2>受控导出</h2>
          </div>
        </div>

        <label class="field compact">
          <span>导出范围</span>
          <select v-model="exportScope">
            <option value="library">整库</option>
            <option value="album">当前相册</option>
            <option value="date-range">时间范围</option>
          </select>
        </label>

        <div v-if="exportScope === 'album'" class="export-note">
          当前将导出：<strong>{{ selectedAlbumName }}</strong>
        </div>

        <div v-if="exportScope === 'date-range'" class="date-grid">
          <label class="field compact">
            <span>开始日期</span>
            <input v-model="exportDateFrom" type="date" />
          </label>
          <label class="field compact">
            <span>结束日期</span>
            <input v-model="exportDateTo" type="date" />
          </label>
        </div>

        <button
          class="primary export-button"
          type="button"
          :disabled="!canExport"
          @click="createExport"
        >
          生成短时效导出包
        </button>

        <div v-if="exportJob" class="export-result">
          <p><strong>导出状态：</strong>{{ exportJob.status }}</p>
          <p><strong>资产数量：</strong>{{ exportJob.assetCount }}</p>
          <p><strong>到期时间：</strong>{{ exportJob.expiresAt }}</p>
          <a :href="exportJob.archiveUrl" target="_blank" rel="noreferrer">下载导出归档</a>
          <a :href="exportJob.redactedManifestUrl" target="_blank" rel="noreferrer">
            下载脱敏 manifest
          </a>
        </div>
      </article>

      <article class="panel">
        <div class="panel-head">
          <div>
            <p class="panel-label">Asset Detail</p>
            <h2>当前资产详情</h2>
          </div>
          <button v-if="selectedAssetDetail" class="ghost" type="button" @click="clearAssetDetail">
            清空
          </button>
        </div>

        <div v-if="detailLoading" class="detail-loading">正在拉取资产详情...</div>
        <div v-else-if="selectedAssetDetail" class="detail-card">
          <div class="detail-preview" :data-ready="Boolean(selectedAssetDetail.thumbnailToken)">
            <strong>{{
              selectedAssetDetail.thumbnailToken ? '缩略图句柄已就绪' : '受控占位展示'
            }}</strong>
            <span>{{ previewStatusLine(selectedAssetDetail) }}</span>
          </div>
          <p class="detail-id">{{ selectedAssetDetail.assetId }}</p>
          <p class="detail-copy">
            {{ selectedAssetDetail.captionPreview || '当前详情已可查询，后台增强仍可能继续推进。' }}
          </p>
          <div class="detail-meta">
            <span>阶段：{{ describeStage(selectedAssetDetail.processingStage) }}</span>
            <span>备份：{{ selectedAssetDetail.backupStatus }}</span>
            <span>媒体类型：{{ selectedAssetDetail.mediaType }}</span>
            <span v-if="selectedAssetDetail.capturedAt">
              拍摄时间：{{ selectedAssetDetail.capturedAt }}
            </span>
          </div>
        </div>
        <p v-else class="empty">
          从时间线卡片点“查看详情”后，这里会显示当前资产的处理状态与占位展示。
        </p>
      </article>
    </div>
  </section>

  <section class="panel selected-album">
    <div class="panel-head">
      <div>
        <p class="panel-label">Selected Album</p>
        <h2>{{ selectedAlbumName }}</h2>
      </div>
      <p class="count">{{ selectedAlbum?.items.length ?? 0 }} 项</p>
    </div>

    <div v-if="selectedAlbum?.items.length" class="album-asset-list">
      <article
        v-for="item in selectedAlbum.items"
        :key="`album-${item.assetId}`"
        class="album-asset-card"
      >
        <strong>{{ item.assetId }}</strong>
        <span>{{ item.processingStage }}</span>
        <small>{{ item.timelineTimestamp }}</small>
      </article>
    </div>
    <p v-else class="empty">选中的相册还没有资产，可以从时间线卡片把照片加入当前相册。</p>
  </section>
</template>

<style scoped>
.hero,
.toolbar,
.dashboard {
  display: grid;
  gap: 24px;
}

.hero {
  grid-template-columns: minmax(0, 1.45fr) minmax(300px, 0.9fr);
  align-items: stretch;
}

.toolbar {
  grid-template-columns: minmax(0, 1.3fr) minmax(320px, 0.9fr);
  margin-top: 24px;
}

.dashboard {
  grid-template-columns: minmax(0, 1.5fr) minmax(320px, 0.95fr);
  margin-top: 24px;
}

.main-column,
.side-column {
  display: grid;
  gap: 24px;
}

.hero-copy,
.hero-card,
.panel,
.search-panel,
.filter-panel {
  border: 1px solid rgba(18, 42, 44, 0.08);
  border-radius: 28px;
  background:
    radial-gradient(circle at top left, rgba(199, 233, 224, 0.75), transparent 38%),
    linear-gradient(155deg, rgba(255, 251, 244, 0.95), rgba(240, 247, 246, 0.92));
  box-shadow: 0 26px 60px rgba(20, 43, 47, 0.09);
}

.hero-copy,
.hero-card,
.panel,
.search-panel,
.filter-panel {
  padding: 28px;
}

.eyebrow,
.card-label,
.panel-label {
  margin: 0 0 10px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  font-size: 0.8rem;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #5f716b;
}

h1 {
  margin: 0;
  max-width: 13ch;
  font-size: clamp(2.4rem, 5vw, 5rem);
  line-height: 0.95;
}

h2 {
  margin: 4px 0 0;
  font-size: 1.3rem;
}

.summary {
  margin: 22px 0 0;
  max-width: 60ch;
  font-size: 1.03rem;
  line-height: 1.75;
}

.hero-actions,
.inline-actions,
.asset-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.hero-actions {
  margin-top: 28px;
}

.primary,
.secondary,
.ghost,
.place-card,
.album-card {
  border: none;
  border-radius: 999px;
  font: inherit;
  cursor: pointer;
}

.primary {
  background: #10343a;
  color: #f6efe5;
  padding: 12px 18px;
  font-weight: 600;
}

.secondary,
.ghost {
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.68);
  color: #17363b;
  border: 1px solid rgba(16, 52, 58, 0.14);
}

.hero-card {
  display: grid;
  gap: 16px;
}

.field {
  display: grid;
  gap: 8px;
}

.field span,
.meta-line,
.asset-time,
.asset-copy,
.asset-meta,
.empty,
.export-note,
.export-result,
.count,
small {
  color: #526365;
}

input,
select {
  border-radius: 18px;
  border: 1px solid rgba(16, 52, 58, 0.12);
  background: rgba(252, 255, 252, 0.88);
  padding: 14px 16px;
  font: inherit;
}

.compact input,
.compact select {
  padding: 12px 14px;
}

.error-banner {
  margin: 24px 0 0;
  padding: 14px 18px;
  border-radius: 18px;
  background: rgba(143, 46, 46, 0.08);
  color: #8a3131;
}

.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 18px;
}

.asset-list,
.duplicate-grid,
.place-list,
.album-list,
.album-asset-list {
  display: grid;
  gap: 16px;
}

.asset-card,
.pair-card,
.album-asset-card {
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.74);
  border: 1px solid rgba(16, 52, 58, 0.08);
  padding: 18px;
}

.asset-preview,
.detail-preview {
  display: grid;
  gap: 6px;
  margin-bottom: 14px;
  padding: 14px 16px;
  border-radius: 18px;
  background: rgba(16, 52, 58, 0.06);
  color: #305258;
}

.asset-preview[data-ready='true'],
.detail-preview[data-ready='true'] {
  background: rgba(51, 127, 89, 0.1);
  color: #23503d;
}

.asset-top,
.pair-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
}

.asset-id {
  margin: 0 0 4px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  font-size: 0.78rem;
  color: #6d7d79;
}

.asset-copy {
  margin: 12px 0;
  line-height: 1.7;
}

.detail-copy {
  margin: 14px 0;
  color: #526365;
  line-height: 1.7;
}

.asset-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 14px;
  font-size: 0.92rem;
}

.detail-meta {
  display: grid;
  gap: 8px;
  color: #526365;
}

.chip {
  padding: 8px 12px;
  border-radius: 999px;
  background: rgba(16, 52, 58, 0.1);
  color: #16363c;
  font-size: 0.85rem;
}

.pair-divider {
  height: 1px;
  margin: 14px 0;
  background: linear-gradient(90deg, transparent, rgba(16, 52, 58, 0.18), transparent);
}

.place-card,
.album-card {
  display: grid;
  gap: 6px;
  align-items: start;
  text-align: left;
  padding: 16px 18px;
  background: rgba(255, 255, 255, 0.68);
  border: 1px solid rgba(16, 52, 58, 0.1);
}

.album-card.selected {
  border-color: rgba(16, 52, 58, 0.35);
  box-shadow: inset 0 0 0 1px rgba(16, 52, 58, 0.12);
}

.album-create {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  margin-bottom: 16px;
}

.date-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.export-button {
  margin-top: 16px;
}

.export-result {
  display: grid;
  gap: 8px;
  margin-top: 16px;
}

.export-result a {
  color: #0f5f66;
  text-decoration: underline;
}

.selected-album {
  margin-top: 24px;
}

.detail-card {
  display: grid;
  gap: 12px;
}

.detail-id,
.detail-loading {
  margin: 0;
  color: #17363b;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
}

@media (max-width: 1024px) {
  .hero,
  .toolbar,
  .dashboard,
  .date-grid {
    grid-template-columns: 1fr;
  }
}
</style>
