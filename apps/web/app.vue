<template>
  <div class="shell">
    <header class="topbar">
      <NuxtLink class="brand" to="/">PhotoNest</NuxtLink>
      <nav class="nav">
        <NuxtLink to="/">总览</NuxtLink>
        <NuxtLink to="/import">导入</NuxtLink>
      </nav>
    </header>
    <main class="content">
      <NuxtPage />
    </main>
  </div>
</template>

<style>
:root {
  color-scheme: light;
  font-family: 'IBM Plex Sans', 'Segoe UI', sans-serif;
  background:
    radial-gradient(circle at top right, rgba(217, 136, 88, 0.16), transparent 32%),
    radial-gradient(circle at left 20%, rgba(70, 137, 158, 0.18), transparent 28%),
    linear-gradient(180deg, #f4efe6 0%, #f8f5ef 40%, #eef2f1 100%);
  color: #1d2b2f;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  min-height: 100vh;
}

a {
  color: inherit;
  text-decoration: none;
}

.shell {
  min-height: 100vh;
  padding: 24px;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  margin: 0 auto 24px;
  max-width: 1180px;
}

.brand {
  font-family: 'Azeret Mono', 'IBM Plex Mono', monospace;
  font-size: 1rem;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.nav {
  display: flex;
  gap: 16px;
  font-size: 0.95rem;
}

.content {
  margin: 0 auto;
  max-width: 1180px;
}

@media (max-width: 720px) {
  .shell {
    padding: 16px;
  }

  .topbar {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
