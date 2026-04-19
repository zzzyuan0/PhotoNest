import { describe, expect, it } from 'vitest';

import {
  buildAssetEmptyState,
  buildImportCompletionSummary,
  collectVisibleClassification,
  describeActionUnavailable,
  describeBackupStatus,
  describePreviewState,
  describeProcessingStage,
  describeRecognitionState,
  describeSearchStatus,
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
    expect(describeProcessingStage('metadata-ready')).toBe('基础整理已完成');
    expect(describeProcessingStage('ai-ready')).toBe('识别结果已生成');
  });

  it('describes backup states without overstating temporary failures', () => {
    expect(describeBackupStatus('pending')).toBe('备份排队中');
    expect(describeBackupStatus('verified')).toBe('已备份');
    expect(describeBackupStatus('failed')).toBe('备份待处理');
  });

  it('explains unavailable actions close to the triggering control', () => {
    expect(describeActionUnavailable('album')).toBe('请先选中一个当前相册，再把照片加入进去。');
    expect(describeActionUnavailable('credentials')).toBe('请先填写用户名和密码，再提交登录。');
  });

  it('summarizes import completion without pretending recognition already finished', () => {
    expect(
      buildImportCompletionSummary({
        completedCount: 2,
        failedCount: 0,
        importedAssetIds: ['a-1', 'a-2'],
      }),
    ).toMatchObject({
      tone: 'success',
      focusTarget: 'a-2',
    });

    expect(
      buildImportCompletionSummary({
        completedCount: 1,
        failedCount: 1,
        importedAssetIds: ['a-1'],
      }),
    ).toMatchObject({
      tone: 'warning',
      cta: '前往照片库查看已成功入库的照片',
    });
  });

  it('turns backend stages into stable user-facing recognition states', () => {
    expect(describeRecognitionState({ processingStage: 'derivatives-ready' })).toMatchObject({
      tone: 'info',
      label: '预览准备中',
    });
    expect(
      describeRecognitionState({
        processingStage: 'partial-failure',
        recognitionStatusNote: 'failed: embedding',
      }),
    ).toMatchObject({
      tone: 'warning',
      label: '部分失败',
    });
  });

  it('collects visible classification signals for the detail panel', () => {
    expect(
      collectVisibleClassification({
        processingStage: 'indexed',
        locationLabel: 'Guangzhou',
        captionPreview: 'sunset over the river',
        ocrPreview: 'Pearl River',
        tags: ['sunset', 'river'],
        semanticTags: ['scene:river', 'activity:walking'],
        searchReady: true,
      }),
    ).toEqual([
      { label: '地点', value: 'Guangzhou' },
      { label: '描述', value: 'sunset over the river' },
      { label: '文字识别', value: 'Pearl River' },
      { label: '语义标签', value: 'scene:river / activity:walking' },
      { label: '搜索准备', value: '已可搜索' },
    ]);
  });

  it('describes whether the asset is searchable yet', () => {
    expect(describeSearchStatus({ processingStage: 'indexed', searchReady: true })).toContain(
      '搜索已就绪',
    );
    expect(
      describeSearchStatus({ processingStage: 'partial-failure', searchReady: false }),
    ).toContain('部分失败');
  });
});
