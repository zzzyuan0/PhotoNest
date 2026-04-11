export default defineNuxtConfig({
  future: {
    compatibilityVersion: 4,
  },
  devtools: {
    enabled: true,
  },
  css: [],
  app: {
    head: {
      title: 'PhotoNest',
      meta: [
        {
          name: 'description',
          content: '统一照片导入、发现、识别与备份的照片主库。',
        },
      ],
    },
  },
  runtimeConfig: {
    public: {
      apiBaseURL: process.env.NUXT_PUBLIC_API_BASE_URL ?? 'http://localhost:8080',
    },
  },
  typescript: {
    strict: true,
  },
});
