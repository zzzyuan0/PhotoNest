<script setup lang="ts">
import { computed, reactive, ref } from 'vue';

import { apiFetch, type AuthSessionResponse } from '../lib/api/client';

const form = reactive({
  username: 'admin',
  password: '',
});

const loading = ref(false);
const error = ref('');
const response = ref<AuthSessionResponse | null>(null);

const session = computed(() => response.value?.session ?? null);

async function submitLogin() {
  loading.value = true;
  error.value = '';

  try {
    response.value = await apiFetch<AuthSessionResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: {
        username: form.username,
        password: form.password,
      },
    });
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : '登录失败，请检查配置或凭据。';
  } finally {
    loading.value = false;
  }
}

async function loadCurrentSession() {
  loading.value = true;
  error.value = '';

  try {
    response.value = await apiFetch<AuthSessionResponse>('/api/v1/auth/session');
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : '当前没有可用会话。';
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <section class="login-shell">
    <div class="login-copy">
      <p class="eyebrow">Access Control</p>
      <h1>先建立可信会话，再打开照片库能力。</h1>
      <p class="summary">
        这一页用于验证本轮新增的 bootstrap 登录、会话 Cookie、Bearer Token 返回以及
        CSRF/近期认证基础设施。
      </p>
      <div class="actions">
        <button class="primary" type="button" :disabled="loading" @click="submitLogin">
          {{ loading ? '处理中...' : '创建登录会话' }}
        </button>
        <button class="secondary" type="button" :disabled="loading" @click="loadCurrentSession">
          读取当前会话
        </button>
      </div>
    </div>

    <form class="login-card" @submit.prevent="submitLogin">
      <label>
        <span>用户名</span>
        <input v-model="form.username" autocomplete="username" />
      </label>

      <label>
        <span>密码</span>
        <input v-model="form.password" type="password" autocomplete="current-password" />
      </label>

      <p v-if="error" class="error">{{ error }}</p>

      <button class="submit" type="submit" :disabled="loading">
        {{ loading ? '处理中...' : '登录并写入会话 Cookie' }}
      </button>
    </form>
  </section>

  <section v-if="session" class="session-panel">
    <p class="panel-label">Current Session</p>
    <div class="session-grid">
      <div>
        <span class="field">Subject</span>
        <strong>{{ session.subjectId }}</strong>
      </div>
      <div>
        <span class="field">Display</span>
        <strong>{{ session.displayName }}</strong>
      </div>
      <div>
        <span class="field">Roles</span>
        <strong>{{ session.roles.join(', ') }}</strong>
      </div>
      <div>
        <span class="field">Libraries</span>
        <strong>{{
          session.libraryIds.length ? session.libraryIds.join(', ') : '全部由角色决定'
        }}</strong>
      </div>
      <div>
        <span class="field">Recent Auth</span>
        <strong>{{ session.recentAuthAt }}</strong>
      </div>
      <div>
        <span class="field">Expires</span>
        <strong>{{ session.expiresAt }}</strong>
      </div>
    </div>
  </section>
</template>

<style scoped>
.login-shell {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(300px, 0.95fr);
  gap: 24px;
  align-items: stretch;
}

.login-copy,
.login-card,
.session-panel {
  border: 1px solid rgba(29, 43, 47, 0.08);
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.78);
  backdrop-filter: blur(12px);
  box-shadow: 0 20px 40px rgba(44, 59, 63, 0.08);
}

.login-copy,
.login-card,
.session-panel {
  padding: 28px;
}

.eyebrow,
.panel-label,
.field {
  margin: 0 0 10px;
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #6a7374;
  font-size: 0.82rem;
}

h1 {
  margin: 0;
  font-size: clamp(2.1rem, 4vw, 4rem);
  line-height: 0.98;
  max-width: 12ch;
}

.summary {
  margin: 20px 0 0;
  max-width: 56ch;
  line-height: 1.7;
}

.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 24px;
}

.primary,
.secondary,
.submit {
  border-radius: 999px;
  padding: 12px 18px;
  font-weight: 600;
  border: none;
}

.primary,
.submit {
  background: #18353b;
  color: #f9f3ea;
}

.secondary {
  background: transparent;
  border: 1px solid rgba(24, 53, 59, 0.18);
}

.login-card {
  display: grid;
  gap: 18px;
}

label {
  display: grid;
  gap: 8px;
}

label span {
  font-size: 0.92rem;
  color: #4a5557;
}

input {
  border-radius: 16px;
  border: 1px solid rgba(24, 53, 59, 0.14);
  background: rgba(248, 252, 252, 0.9);
  padding: 14px 16px;
  font: inherit;
}

.error {
  margin: 0;
  color: #8b2b2b;
  line-height: 1.6;
}

.session-panel {
  margin-top: 24px;
}

.session-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px 24px;
}

.session-grid div {
  display: grid;
  gap: 6px;
}

strong {
  line-height: 1.6;
  word-break: break-word;
}

@media (max-width: 900px) {
  .login-shell,
  .session-grid {
    grid-template-columns: 1fr;
  }
}
</style>
