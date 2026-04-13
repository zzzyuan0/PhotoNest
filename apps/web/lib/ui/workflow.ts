export type UploadStatus =
  | 'pending'
  | 'hashing'
  | 'planning'
  | 'uploading'
  | 'confirming'
  | 'done'
  | 'error';

export type UploadEntryLike = {
  status: UploadStatus;
  message: string;
};

export type PreviewableAsset = {
  processingStage: string;
  thumbnailToken?: string;
};

export type PreviewTone = 'ready' | 'processing' | 'warning';

export function describeProcessingStage(value: string) {
  switch (value) {
    case 'accepted':
      return '已接收';
    case 'stored':
      return '已入库';
    case 'derivatives-ready':
      return '预览已准备';
    case 'metadata-ready':
      return '元数据整理中';
    case 'ai-ready':
      return 'AI 结果已就绪';
    case 'indexed':
      return '已可搜索';
    case 'partial-failure':
      return '需要人工留意';
    default:
      return value.replaceAll('-', ' ');
  }
}

export function describeBackupStatus(value: string) {
  switch (value) {
    case 'pending':
      return '备份排队中';
    case 'completed':
      return '已备份';
    case 'failed':
      return '备份异常';
    default:
      return value.replaceAll('-', ' ');
  }
}

export function describeUploadStatus(status: UploadStatus) {
  switch (status) {
    case 'pending':
      return {
        step: '等待开始',
        detail: '文件已加入队列，等待你开始导入。',
        tone: 'idle',
      } as const;
    case 'hashing':
      return {
        step: '校验文件',
        detail: '正在为文件生成校验摘要，确保上传内容一致。',
        tone: 'active',
      } as const;
    case 'planning':
      return {
        step: '准备上传',
        detail: '正在向服务端申请上传票据与目标位置。',
        tone: 'active',
      } as const;
    case 'uploading':
      return {
        step: '传输文件',
        detail: '浏览器正在把照片发送到对象存储。',
        tone: 'active',
      } as const;
    case 'confirming':
      return {
        step: '确认入库',
        detail: '上传完成，正在通知系统登记这张照片。',
        tone: 'active',
      } as const;
    case 'done':
      return {
        step: '导入完成',
        detail: '文件已经进入照片库，可以继续查看或整理。',
        tone: 'success',
      } as const;
    case 'error':
      return {
        step: '需要处理',
        detail: '这一项没有成功完成，需要检查提示后重试。',
        tone: 'danger',
      } as const;
  }
}

export function summarizeUploadQueue(entries: UploadEntryLike[]) {
  const summary = {
    total: entries.length,
    done: 0,
    failed: 0,
    active: 0,
    pending: 0,
  };

  for (const entry of entries) {
    if (entry.status === 'done') {
      summary.done += 1;
      continue;
    }
    if (entry.status === 'error') {
      summary.failed += 1;
      continue;
    }
    if (entry.status === 'pending') {
      summary.pending += 1;
      continue;
    }
    summary.active += 1;
  }

  return summary;
}

export function describePreviewState(item: PreviewableAsset): {
  tone: PreviewTone;
  label: string;
  detail: string;
} {
  if (item.thumbnailToken) {
    return {
      tone: 'ready',
      label: '可以预览',
      detail: '缩略图已经准备好，可以安心继续浏览和整理这张照片。',
    };
  }

  if (item.processingStage === 'partial-failure') {
    return {
      tone: 'warning',
      label: '预览受限',
      detail: '这张照片已经存在，但预览生成遇到问题，建议稍后重试或继续其他操作。',
    };
  }

  return {
    tone: 'processing',
    label: '正在准备预览',
    detail: '照片已经入库，系统仍在生成缩略图和后续处理结果。',
  };
}

export function pickNextSelectedAsset(items: Array<{ assetId: string }>, currentAssetId: string) {
  if (items.length === 0) {
    return '';
  }

  if (currentAssetId && items.some((item) => item.assetId === currentAssetId)) {
    return currentAssetId;
  }

  return items[0]?.assetId ?? '';
}

export function buildAssetEmptyState(input: {
  hasSession: boolean;
  hasLibrary: boolean;
  isSearching: boolean;
  hasFilters: boolean;
}) {
  if (!input.hasSession) {
    return {
      title: '先登录，再打开照片库',
      detail: '登录后你才能读取照片库、查看详情并继续导入新照片。',
      primaryAction: '前往登录',
      secondaryAction: '了解导入流程',
    };
  }

  if (!input.hasLibrary) {
    return {
      title: '先选择一个可访问的照片库',
      detail: '填写或确认 library ID 后，就能读取时间线、搜索结果和详情面板。',
      primaryAction: '填写照片库',
      secondaryAction: '去登录页查看会话',
    };
  }

  if (input.isSearching || input.hasFilters) {
    return {
      title: '没有找到符合条件的照片',
      detail: '可以清空搜索词、切换筛选条件，或者去导入一批新照片再回来查看。',
      primaryAction: '清空条件',
      secondaryAction: '去导入照片',
    };
  }

  return {
    title: '照片列表暂时还是空的',
    detail: '现在可以先导入一批照片，等系统入库后这里会自动成为你的浏览入口。',
    primaryAction: '去导入照片',
    secondaryAction: '刷新照片库',
  };
}
