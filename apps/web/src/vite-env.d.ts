/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string;
  readonly VITE_API_PROXY_TARGET?: string;
  readonly VITE_ANKI_CONNECT_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
