import { describe, expect, it } from 'vitest';

import {
  buildAssetEmptyState,
  describePreviewState,
  describeProcessingStage,
  describeUploadStatus,
  pickNextSelectedAsset,
  summarizeUploadQueue,
} from './workflow';

describe('workflow helpers', () => {
  it('maps upload states to user-friendly steps', () => {
    expect(describeUploadStatus('uploading')).toMatchObject({
      step: '传输文件',
      tone: 'active',
    });
    expect(describeUploadStatus('done')).toMatchObject({
      step: '导入完成',
      tone: 'success',
    });
  });

  it('summarizes queue progress for import feedback', () => {
    expect(
      summarizeUploadQueue([
        { status: 'pending', message: '' },
        { status: 'uploading', message: '' },
        { status: 'done', message: '' },
        { status: 'error', message: '' },
      ]),
    ).toEqual({
      total: 4,
      done: 1,
      failed: 1,
      active: 1,
      pending: 1,
    });
  });

  it('describes preview availability clearly', () => {
    expect(describePreviewState({ processingStage: 'indexed', thumbnailToken: 'thumb' })).toEqual({
      tone: 'ready',
      label: '可以预览',
      detail: '缩略图已经准备好，可以安心继续浏览和整理这张照片。',
    });

    expect(describePreviewState({ processingStage: 'partial-failure' })).toEqual({
      tone: 'warning',
      label: '预览受限',
      detail: '这张照片已经存在，但预览生成遇到问题，建议稍后重试或继续其他操作。',
    });
  });

  it('keeps current asset selection when possible', () => {
    expect(pickNextSelectedAsset([{ assetId: 'a1' }, { assetId: 'a2' }], 'a2')).toBe('a2');
    expect(pickNextSelectedAsset([{ assetId: 'a1' }, { assetId: 'a2' }], 'missing')).toBe('a1');
    expect(pickNextSelectedAsset([], 'a2')).toBe('');
  });

  it('builds actionable empty states for dashboard edge cases', () => {
    expect(
      buildAssetEmptyState({
        hasSession: false,
        hasLibrary: false,
        isSearching: false,
        hasFilters: false,
      }),
    ).toMatchObject({
      title: '先登录，再打开照片库',
      primaryAction: '前往登录',
    });

    expect(
      buildAssetEmptyState({
        hasSession: true,
        hasLibrary: true,
        isSearching: true,
        hasFilters: false,
      }),
    ).toMatchObject({
      title: '没有找到符合条件的照片',
      secondaryAction: '去导入照片',
    });
  });

  it('keeps stage labels task-oriented', () => {
    expect(describeProcessingStage('metadata-ready')).toBe('元数据整理中');
    expect(describeProcessingStage('ai-ready')).toBe('AI 结果已就绪');
  });
});
