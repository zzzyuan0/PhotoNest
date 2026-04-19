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

export type RecognizableAsset = PreviewableAsset & {
  captionPreview?: string;
  ocrPreview?: string;
  locationLabel?: string;
  tags?: string[];
  semanticTags?: string[];
  searchReady?: boolean;
  recognitionStatusNote?: string;
};

export type PreviewTone = 'ready' | 'processing' | 'warning';

export type ActionFeedbackTone = 'info' | 'success' | 'warning' | 'danger';

export function describeProcessingStage(value: string) {
  switch (value) {
    case 'accepted':
      return '已接收';
    case 'stored':
      return '已入库';
    case 'derivatives-ready':
      return '预览准备中';
    case 'metadata-ready':
      return '基础整理已完成';
    case 'ai-ready':
      return '识别结果已生成';
    case 'indexed':
      return '已可搜索与整理';
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
    case 'verified':
    case 'completed':
      return '已备份';
    case 'failed':
      return '备份待处理';
    case '':
      return '暂无备份信息';
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

export function describeActionUnavailable(
  requirement: 'session' | 'library' | 'files' | 'album' | 'credentials',
) {
  switch (requirement) {
    case 'session':
      return '请先完成登录，再执行这个动作。';
    case 'library':
      return '请先填写或确认一个可访问的 library ID。';
    case 'files':
      return '请先选择至少一个照片文件，再开始导入。';
    case 'album':
      return '请先选中一个当前相册，再把照片加入进去。';
    case 'credentials':
      return '请先填写用户名和密码，再提交登录。';
  }
}

export function describeRecognitionState(item: RecognizableAsset): {
  tone: ActionFeedbackTone;
  label: string;
  detail: string;
} {
  switch (item.processingStage) {
    case 'accepted':
    case 'stored':
      return {
        tone: 'info',
        label: '刚入库',
        detail: '照片已经被系统接收，预览和后续识别仍会继续准备。',
      };
    case 'derivatives-ready':
      return {
        tone: 'info',
        label: '预览准备中',
        detail: '照片已经出现在照片库里，缩略图和基础可见性已经就位，识别分类还在后台继续。',
      };
    case 'metadata-ready':
      return {
        tone: 'info',
        label: '识别进行中',
        detail: '拍摄时间、设备或地点等基础信息已经补齐，caption、OCR 和搜索准备仍在继续。',
      };
    case 'ai-ready':
      return {
        tone: 'success',
        label: '识别结果已生成',
        detail: 'caption、OCR 或标签等结果已经可见，系统正在把这些结果整理进搜索和浏览入口。',
      };
    case 'indexed':
      return {
        tone: 'success',
        label: '已可搜索',
        detail: '这张照片已经完成当前版本的整理闭环，可以通过搜索、地点、收藏或相册继续查找。',
      };
    case 'partial-failure':
      return {
        tone: 'warning',
        label: '部分失败',
        detail:
          item.recognitionStatusNote && item.recognitionStatusNote.trim() !== ''
            ? `照片仍然保留在列表中，但有部分识别阶段失败：${item.recognitionStatusNote}。`
            : '照片仍然保留在列表中，但部分识别阶段失败，建议稍后留意结果是否补齐。',
      };
    default:
      return {
        tone: 'info',
        label: describeProcessingStage(item.processingStage),
        detail: '系统仍在继续推进这张照片的可见结果。',
      };
  }
}

export function describeSearchStatus(
  item: Pick<RecognizableAsset, 'searchReady' | 'processingStage'>,
) {
  if (item.searchReady) {
    return '搜索已就绪，可以直接在上方搜索框中找到这张照片。';
  }
  if (item.processingStage === 'partial-failure') {
    return '搜索整理受部分失败影响，结果可能不完整，但照片仍保留在当前列表。';
  }
  return '搜索整理仍在后台继续，等状态推进到“已可搜索与整理”后会更稳定。';
}

export function collectVisibleClassification(item: RecognizableAsset) {
  const entries: Array<{ label: string; value: string }> = [];

  if (item.locationLabel) {
    entries.push({ label: '地点', value: item.locationLabel });
  }
  if (item.captionPreview) {
    entries.push({ label: '描述', value: item.captionPreview });
  }
  if (item.ocrPreview) {
    entries.push({ label: '文字识别', value: item.ocrPreview });
  }
  if (item.semanticTags?.length) {
    entries.push({ label: '语义标签', value: item.semanticTags.join(' / ') });
  } else if (item.tags?.length) {
    entries.push({ label: '标签', value: item.tags.join(' / ') });
  }
  entries.push({ label: '搜索准备', value: item.searchReady ? '已可搜索' : '仍在整理中' });

  return entries;
}

export function buildImportCompletionSummary(input: {
  completedCount: number;
  failedCount: number;
  importedAssetIds: string[];
}) {
  const importedCount = input.importedAssetIds.length;
  const focusTarget = input.importedAssetIds.at(-1) ?? '';

  if (input.failedCount > 0) {
    return {
      tone: 'warning' as const,
      title: '本轮导入已结束，已成功入库的照片现在可以查看。',
      detail: `成功 ${input.completedCount} 个，需留意 ${input.failedCount} 个。已成功的照片已经进入照片库，识别分类会继续在后台推进。`,
      focusTarget,
      cta: importedCount > 0 ? '前往照片库查看已成功入库的照片' : '继续留在导入页检查失败项',
    };
  }

  return {
    tone: 'success' as const,
    title: '照片已入库，可立即去照片库查看。',
    detail: `本轮成功导入 ${input.completedCount} 个文件。它们已经进入照片库，caption、地点、搜索整理等识别分类结果仍可能继续在后台生成。`,
    focusTarget,
    cta: importedCount > 0 ? '前往照片库查看本轮新照片' : '前往照片库',
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
