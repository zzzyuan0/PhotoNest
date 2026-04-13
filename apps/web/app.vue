<script setup lang="ts">
const route = useRoute();

const navItems = [
  {
    to: '/',
    label: '照片库',
    summary: '浏览、筛选和查看当前照片库',
  },
  {
    to: '/import',
    label: '导入照片',
    summary: '上传新照片并跟进导入进度',
  },
  {
    to: '/login',
    label: '登录',
    summary: '登录后解锁照片浏览与导入能力',
  },
];

const currentSection = computed(() => {
  const matchedItem = navItems.find((item) => item.to === route.path);
  return matchedItem || navItems[0];
});
</script>

<template>
  <div class="app-shell">
    <header class="shell-header">
      <div class="brand-block">
        <NuxtLink class="brand-link" to="/">PhotoNest</NuxtLink>
        <p class="brand-copy">把登录、导入和浏览整理成一条顺手的照片工作流。</p>
      </div>

      <nav class="shell-nav" aria-label="主导航">
        <NuxtLink
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="shell-nav-link"
          :class="{ active: route.path === item.to }"
        >
          <strong>{{ item.label }}</strong>
          <span>{{ item.summary }}</span>
        </NuxtLink>
      </nav>
    </header>

    <main class="shell-main">
      <section class="shell-context">
        <div>
          <p class="shell-kicker">当前区域</p>
          <h1>{{ currentSection.label }}</h1>
        </div>
        <p class="shell-context-copy">{{ currentSection.summary }}</p>
      </section>

      <NuxtPage />
    </main>
  </div>
</template>

<style>
:root {
  color-scheme: light;
  font-family: 'IBM Plex Sans', 'Segoe UI', sans-serif;
  background:
    radial-gradient(circle at top right, rgba(219, 146, 79, 0.18), transparent 30%),
    radial-gradient(circle at left 15%, rgba(71, 124, 145, 0.18), transparent 28%),
    linear-gradient(180deg, #f3ece1 0%, #f8f4ed 45%, #edf3f1 100%);
  color: #183036;
  --shell-border: rgba(24, 48, 54, 0.1);
  --shell-panel: rgba(255, 255, 255, 0.78);
  --shell-panel-strong: rgba(255, 255, 255, 0.92);
  --shell-muted: #5d6c71;
  --shell-primary: #16393f;
  --shell-primary-soft: rgba(22, 57, 63, 0.08);
  --shell-accent: #285c64;
  --shell-success: #225c43;
  --shell-warning: #8e5b1f;
  --shell-danger: #8f3333;
}

* {
  box-sizing: border-box;
}

html,
body,
#__nuxt {
  min-height: 100%;
}

body {
  margin: 0;
}

a {
  color: inherit;
  text-decoration: none;
}

button,
input,
select {
  font: inherit;
}

.app-shell {
  min-height: 100vh;
  padding: 24px;
}

.shell-header,
.shell-context,
.surface-card {
  border: 1px solid var(--shell-border);
  border-radius: 30px;
  background:
    radial-gradient(circle at top left, rgba(208, 235, 229, 0.54), transparent 42%),
    linear-gradient(155deg, rgba(255, 251, 244, 0.94), rgba(246, 250, 249, 0.9));
  box-shadow: 0 24px 60px rgba(22, 44, 49, 0.08);
}

.shell-header,
.shell-context {
  max-width: 1240px;
  margin: 0 auto;
  padding: 24px 28px;
}

.shell-header {
  display: grid;
  grid-template-columns: minmax(260px, 0.8fr) minmax(0, 1.3fr);
  gap: 24px;
  align-items: center;
}

.brand-block {
  display: grid;
  gap: 10px;
}

.brand-link,
.shell-kicker {
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.brand-link {
  font-size: 1rem;
}

.brand-copy,
.shell-context-copy,
.subtle-copy,
.empty-copy,
.status-copy {
  margin: 0;
  color: var(--shell-muted);
  line-height: 1.65;
}

.shell-nav {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.shell-nav-link {
  display: grid;
  gap: 6px;
  padding: 16px 18px;
  border-radius: 22px;
  border: 1px solid transparent;
  background: rgba(255, 255, 255, 0.52);
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    background 180ms ease;
}

.shell-nav-link strong {
  font-size: 1rem;
}

.shell-nav-link span {
  color: var(--shell-muted);
  font-size: 0.92rem;
  line-height: 1.5;
}

.shell-nav-link:hover,
.shell-nav-link.active {
  transform: translateY(-1px);
  border-color: rgba(22, 57, 63, 0.18);
  background: rgba(255, 255, 255, 0.78);
}

.shell-nav-link.active {
  box-shadow: inset 0 0 0 1px rgba(22, 57, 63, 0.08);
}

.shell-main {
  max-width: 1240px;
  margin: 24px auto 0;
}

.shell-context {
  display: flex;
  justify-content: space-between;
  gap: 20px;
  align-items: end;
  margin-bottom: 24px;
}

.shell-kicker {
  margin: 0 0 10px;
  color: var(--shell-muted);
  font-size: 0.8rem;
}

.shell-context h1 {
  margin: 0;
  font-size: clamp(2rem, 4vw, 3.6rem);
  line-height: 0.95;
}

.shell-context-copy {
  max-width: 30rem;
}

.button-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.button-primary,
.button-secondary,
.button-ghost {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 46px;
  padding: 0 18px;
  border-radius: 999px;
  border: 1px solid transparent;
  cursor: pointer;
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    background 180ms ease;
}

.button-primary {
  background: var(--shell-primary);
  color: #f6efe6;
  font-weight: 600;
}

.button-secondary,
.button-ghost {
  background: rgba(255, 255, 255, 0.72);
  color: var(--shell-primary);
  border-color: rgba(22, 57, 63, 0.14);
}

.button-primary:hover,
.button-secondary:hover,
.button-ghost:hover {
  transform: translateY(-1px);
}

.button-primary:disabled,
.button-secondary:disabled,
.button-ghost:disabled {
  cursor: not-allowed;
  opacity: 0.58;
  transform: none;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: fit-content;
  padding: 10px 14px;
  border-radius: 999px;
  font-size: 0.9rem;
  background: rgba(22, 57, 63, 0.08);
  color: var(--shell-primary);
}

.status-pill[data-tone='success'] {
  background: rgba(34, 92, 67, 0.1);
  color: var(--shell-success);
}

.status-pill[data-tone='warning'] {
  background: rgba(142, 91, 31, 0.12);
  color: var(--shell-warning);
}

.status-pill[data-tone='danger'] {
  background: rgba(143, 51, 51, 0.1);
  color: var(--shell-danger);
}

.alert-banner {
  margin: 0;
  padding: 14px 18px;
  border-radius: 18px;
  line-height: 1.6;
}

.alert-banner[data-tone='danger'] {
  background: rgba(143, 51, 51, 0.1);
  color: var(--shell-danger);
}

.alert-banner[data-tone='success'] {
  background: rgba(34, 92, 67, 0.1);
  color: var(--shell-success);
}

.section-heading {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 18px;
}

.section-heading h2,
.section-heading h3,
.section-heading p {
  margin: 0;
}

.section-heading p {
  color: var(--shell-muted);
  line-height: 1.55;
}

.mono-label {
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  font-size: 0.8rem;
  color: var(--shell-muted);
}

.field-group {
  display: grid;
  gap: 8px;
}

.field-label {
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--shell-primary);
}

.text-input,
.select-input {
  width: 100%;
  border-radius: 18px;
  border: 1px solid rgba(22, 57, 63, 0.14);
  background: rgba(252, 255, 252, 0.92);
  padding: 14px 16px;
  color: var(--shell-primary);
}

.empty-state {
  display: grid;
  gap: 16px;
  padding: 24px;
  border-radius: 24px;
  background: rgba(255, 255, 255, 0.62);
  border: 1px dashed rgba(22, 57, 63, 0.16);
}

.empty-state h3,
.empty-state p {
  margin: 0;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 14px;
}

.metric-card {
  display: grid;
  gap: 6px;
  padding: 16px 18px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.68);
  border: 1px solid rgba(22, 57, 63, 0.08);
}

.metric-card strong {
  font-size: 1.2rem;
}

.metric-card span {
  color: var(--shell-muted);
  line-height: 1.45;
}

@media (max-width: 960px) {
  .app-shell {
    padding: 16px;
  }

  .shell-header,
  .shell-context {
    padding: 20px;
  }

  .shell-header,
  .shell-context,
  .shell-nav {
    grid-template-columns: 1fr;
  }

  .shell-context {
    align-items: start;
  }
}

@media (max-width: 720px) {
  .shell-nav-link {
    padding: 14px 16px;
  }

  .button-primary,
  .button-secondary,
  .button-ghost {
    width: 100%;
  }
}
</style>
