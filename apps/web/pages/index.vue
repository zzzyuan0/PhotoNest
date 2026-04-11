<script setup lang="ts">
const milestones = [
  {
    title: '导入状态机',
    body: '服务端创建导入会话，前端直传对象存储，确认后再进入后台处理。',
  },
  {
    title: '识别增强',
    body: 'EXIF、OCR、caption 和 embedding 均通过异步阶段推进，支持部分失败。',
  },
  {
    title: '私有访问',
    body: '默认私有读、短时签名和字段最小返回是整个主库的基线约束。',
  },
];

const tasks = [
  'Nuxt Web、Go API 与 Worker 骨架',
  'OpenAPI 契约与 TypeScript 类型生成',
  '数据库迁移、sqlc 查询组织与本地依赖',
];
</script>

<template>
  <section class="hero">
    <div class="hero-copy">
      <p class="eyebrow">MVP Scaffold</p>
      <h1>让分散照片先有一个稳定、可检索、可恢复的主库。</h1>
      <p class="summary">
        当前页面对应 OpenSpec 里的基础设施阶段，先把上传、处理、发现和备份四条主链路的
        契约与项目结构搭起来。
      </p>
      <div class="actions">
        <NuxtLink class="primary" to="/import">查看导入链路</NuxtLink>
        <a class="secondary" href="/api/v1/health" target="_blank" rel="noreferrer">
          系统健康接口
        </a>
      </div>
    </div>
    <aside class="hero-card">
      <p class="card-label">本轮落地范围</p>
      <ul>
        <li v-for="item in tasks" :key="item">{{ item }}</li>
      </ul>
    </aside>
  </section>

  <section class="grid">
    <article v-for="item in milestones" :key="item.title" class="panel">
      <p class="panel-label">{{ item.title }}</p>
      <p>{{ item.body }}</p>
    </article>
  </section>
</template>

<style scoped>
.hero {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(280px, 0.9fr);
  gap: 24px;
  align-items: stretch;
}

.hero-copy,
.hero-card,
.panel {
  border: 1px solid rgba(29, 43, 47, 0.08);
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.7);
  backdrop-filter: blur(12px);
  box-shadow: 0 20px 40px rgba(44, 59, 63, 0.08);
}

.hero-copy {
  padding: 40px;
}

.eyebrow,
.card-label,
.panel-label {
  margin: 0 0 12px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #6a7374;
  font-size: 0.82rem;
}

h1 {
  margin: 0;
  font-size: clamp(2.3rem, 5vw, 4.8rem);
  line-height: 0.96;
  max-width: 14ch;
}

.summary {
  margin: 24px 0 0;
  max-width: 56ch;
  font-size: 1.05rem;
  line-height: 1.7;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 28px;
}

.primary,
.secondary {
  border-radius: 999px;
  padding: 12px 18px;
  font-weight: 600;
}

.primary {
  background: #18353b;
  color: #f9f3ea;
}

.secondary {
  border: 1px solid rgba(24, 53, 59, 0.18);
}

.hero-card {
  padding: 28px;
}

.hero-card ul {
  margin: 0;
  padding-left: 18px;
  line-height: 1.9;
}

.grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
  margin-top: 24px;
}

.panel {
  padding: 24px;
  line-height: 1.75;
}

@media (max-width: 900px) {
  .hero,
  .grid {
    grid-template-columns: 1fr;
  }

  .hero-copy {
    padding: 28px;
  }
}
</style>
