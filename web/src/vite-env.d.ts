/// <reference types="vite/client" />

// 由 vite.config.ts 的 define 注入（构建时间）
declare const __BUILD_ID__: string

declare module '*.md?raw' {
  const src: string
  export default src
}
