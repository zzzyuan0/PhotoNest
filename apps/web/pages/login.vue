<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';

import { apiFetch, type AuthSessionResponse } from '../lib/api/client';

const form = reactive({
  username: 'admin',
  password: '',
});

const pendingAction = ref<'login' | 'session' | null>(null);
const error = ref('');
const response = ref<AuthSessionResponse | null>(null);
const sessionChecked = ref(false);
const loginSucceeded = ref(false);

const session = computed(() => response.value?.session ?? null);
const hasSession = computed(() => session.value !== null);
const canSubmit = computed(
  () => pendingAction.value === null && form.username.trim() !== '' && form.password.trim() !== '',
);
const sessionLibraries = computed(() =>
  session.value?.libraryIds.length ? session.value.libraryIds.join(', ') : '由当前角色自动决定',
);
const sessionSummary = computed(() => {
  if (!session.value) {
    return [];
  }

  return [
    {
      label: '当前身份',
      value: session.value.displayName,
    },
    {
      label: '可访问照片库',
      value: sessionLibraries.value,
    },
    {
      label: '登录方式',
      value: session.value.authMethod,
    },
    {
      label: '会话有效期',
      value: session.value.expiresAt,
    },
  ];
});

async function submitLogin() {
  pendingAction.value = 'login';
  error.value = '';
  loginSucceeded.value = false;

  try {
    response.value = await apiFetch<AuthSessionResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: {
        username: form.username.trim(),
        password: form.password,
      },
    });
    loginSucceeded.value = true;
  } catch (caught) {
    error.value =
      caught instanceof Error ? caught.message : '登录失败，请检查用户名、密码或服务状态。';
  } finally {
    sessionChecked.value = true;
    pendingAction.value = null;
  }
}

async function loadCurrentSession(options?: { silent?: boolean }) {
  pendingAction.value = 'session';
  if (!options?.silent) {
    error.value = '';
  }

  try {
    response.value = await apiFetch<AuthSessionResponse>('/api/v1/auth/session');
  } catch (caught) {
    response.value = null;
    if (!options?.silent) {
      error.value = caught instanceof Error ? caught.message : '当前还没有可用会话。';
    }
  } finally {
    sessionChecked.value = true;
    pendingAction.value = null;
  }
}

onMounted(async () => {
  await loadCurrentSession({ silent: true });
});
</script>

<template>
  <section class="login-layout">
    <article class="surface-card intro-panel">
      <p class="mono-label">登录照片工作台</p>
      <h2>登录后，你就能浏览照片库、继续导入，并在同一个界面里查看整理结果。</h2>
      <p class="subtle-copy">
        这里只保留你真正需要关心的信息：输入凭据、确认当前身份，然后决定下一步去浏览照片还是继续导入。
      </p>

      <div class="metric-grid">
        <div class="metric-card">
          <strong>浏览照片</strong>
          <span>登录后可读取时间线、搜索结果和详情面板。</span>
        </div>
        <div class="metric-card">
          <strong>继续导入</strong>
          <span>会话建立后，可以直接前往导入页上传新照片。</span>
        </div>
        <div class="metric-card">
          <strong>确认身份</strong>
          <span>页面会展示当前登录身份和可访问的照片库范围。</span>
        </div>
      </div>

      <div class="button-row">
        <NuxtLink class="button-secondary" to="/">先看看照片库</NuxtLink>
        <NuxtLink class="button-secondary" to="/import">查看导入流程</NuxtLink>
        <button
          class="button-ghost"
          type="button"
          :disabled="pendingAction !== null"
          @click="loadCurrentSession()"
        >
          {{ pendingAction === 'session' ? '正在读取会话…' : '读取当前会话' }}
        </button>
      </div>
    </article>

    <article class="surface-card form-panel">
      <div class="section-heading">
        <div>
          <p class="mono-label">登录表单</p>
          <h2>使用账号密码进入工作台</h2>
        </div>
        <span class="status-pill" :data-tone="hasSession ? 'success' : 'warning'">
          {{ hasSession ? '已检测到有效会话' : '当前还未登录' }}
        </span>
      </div>

      <form class="login-form" @submit.prevent="submitLogin">
        <label class="field-group">
          <span class="field-label">用户名</span>
          <input v-model="form.username" class="text-input" autocomplete="username" />
        </label>

        <label class="field-group">
          <span class="field-label">密码</span>
          <input
            v-model="form.password"
            class="text-input"
            type="password"
            autocomplete="current-password"
          />
        </label>

        <p class="status-copy">
          登录成功后，这里会展示你当前的身份摘要，并给出进入照片库或导入页的下一步入口。
        </p>

        <p v-if="error" class="alert-banner" data-tone="danger">{{ error }}</p>
        <p v-else-if="loginSucceeded" class="alert-banner" data-tone="success">
          登录成功，当前会话已经就绪。你现在可以进入照片库或继续导入新照片。
        </p>

        <div class="button-row">
          <button class="button-primary" type="submit" :disabled="!canSubmit">
            {{ pendingAction === 'login' ? '正在登录…' : '登录并进入工作台' }}
          </button>
          <button
            class="button-secondary"
            type="button"
            :disabled="pendingAction !== null"
            @click="loadCurrentSession()"
          >
            {{ pendingAction === 'session' ? '读取中…' : '查看已有会话' }}
          </button>
        </div>
      </form>
    </article>
  </section>

  <section class="session-layout">
    <article class="surface-card session-panel">
      <div class="section-heading">
        <div>
          <p class="mono-label">当前会话摘要</p>
          <h2>{{ hasSession ? '你已经可以开始处理照片' : '还没有检测到可用会话' }}</h2>
        </div>
      </div>

      <div v-if="hasSession" class="metric-grid">
        <div v-for="item in sessionSummary" :key="item.label" class="metric-card">
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
        </div>
      </div>

      <div v-else class="empty-state">
        <h3>{{ sessionChecked ? '先完成登录' : '正在检查当前会话' }}</h3>
        <p class="empty-copy">
          {{
            sessionChecked
              ? '如果你还没有登录，请填写上方表单。登录成功后，这里会显示你的身份摘要和后续入口。'
              : '页面会先尝试读取已有会话，若没有会话，你可以直接在上方输入凭据。'
          }}
        </p>
        <div class="button-row">
          <button
            class="button-secondary"
            type="button"
            :disabled="pendingAction !== null"
            @click="loadCurrentSession()"
          >
            {{ pendingAction === 'session' ? '检查中…' : '再次检查会话' }}
          </button>
          <NuxtLink class="button-ghost" to="/import">先查看导入页</NuxtLink>
        </div>
      </div>
    </article>

    <article class="surface-card next-panel">
      <div class="section-heading">
        <div>
          <p class="mono-label">下一步</p>
          <h2>登录之后要去哪里</h2>
        </div>
      </div>

      <div class="next-actions">
        <NuxtLink class="next-link" to="/">
          <strong>进入照片库</strong>
          <span>查看时间线、切换详情、把照片加入收藏或相册。</span>
        </NuxtLink>
        <NuxtLink class="next-link" to="/import">
          <strong>继续导入照片</strong>
          <span>打开步骤化导入流程，上传新照片并跟进处理进度。</span>
        </NuxtLink>
      </div>
    </article>
  </section>
</template>

<style scoped>
.login-layout,
.session-layout {
  display: grid;
  gap: 24px;
}

.login-layout {
  grid-template-columns: minmax(0, 1.2fr) minmax(320px, 0.9fr);
}

.session-layout {
  grid-template-columns: minmax(0, 1.1fr) minmax(280px, 0.9fr);
  margin-top: 24px;
}

.intro-panel,
.form-panel,
.session-panel,
.next-panel {
  padding: 28px;
}

.intro-panel,
.session-panel {
  display: grid;
  gap: 24px;
}

.intro-panel h2,
.form-panel h2,
.session-panel h2,
.next-panel h2 {
  margin: 0;
  font-size: clamp(1.6rem, 3vw, 2.5rem);
  line-height: 1.1;
}

.login-form {
  display: grid;
  gap: 18px;
}

.next-actions {
  display: grid;
  gap: 14px;
}

.next-link {
  display: grid;
  gap: 6px;
  padding: 18px;
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.7);
  border: 1px solid rgba(22, 57, 63, 0.1);
}

.next-link strong {
  font-size: 1rem;
}

.next-link span {
  color: #5d6c71;
  line-height: 1.55;
}

@media (max-width: 980px) {
  .login-layout,
  .session-layout {
    grid-template-columns: 1fr;
  }

  .intro-panel,
  .form-panel,
  .session-panel,
  .next-panel {
    padding: 22px;
  }
}
</style>
