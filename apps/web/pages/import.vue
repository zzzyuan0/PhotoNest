<script setup lang="ts">
const steps = [
  '创建导入会话并绑定照片库范围',
  '服务端签发对象 key 与最小权限上传凭据',
  '浏览器直传对象存储并回调确认',
  '后台进入派生资源、元数据、AI 和索引阶段',
];
</script>

<template>
  <section class="wrap">
    <div class="headline">
      <p class="label">Upload Pipeline</p>
      <h1>Web 上传先保证闭环，再谈智能增强。</h1>
      <p>
        这页是 Nuxt 客户端的最小占位界面，后续会在这里挂接 OpenAPI 生成的 TypeScript
        客户端和真实上传流程。
      </p>
    </div>

    <ol class="timeline">
      <li v-for="step in steps" :key="step">{{ step }}</li>
    </ol>
  </section>
</template>

<style scoped>
.wrap {
  border-radius: 28px;
  padding: 28px;
  background: rgba(255, 255, 255, 0.76);
  box-shadow: 0 20px 40px rgba(44, 59, 63, 0.08);
}

.label {
  margin: 0 0 10px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #6a7374;
  font-size: 0.82rem;
}

h1 {
  margin: 0 0 16px;
  font-size: clamp(2rem, 4vw, 3.5rem);
  line-height: 1.05;
  max-width: 12ch;
}

p {
  line-height: 1.7;
  max-width: 62ch;
}

.timeline {
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 14px;
}

.timeline li {
  border-radius: 20px;
  padding: 18px 20px;
  background: linear-gradient(135deg, rgba(232, 244, 246, 0.92), rgba(255, 249, 241, 0.92));
  border: 1px solid rgba(24, 53, 59, 0.08);
}
</style>
