/**
 * 用 .mjs 而不是 .ts：`next start` 在启动时会重新加载这份配置，
 * 而 TypeScript 只是 devDependency，生产镜像里装的是 --prod 依赖，
 * 用 .ts 会在启动时报「找不到 typescript」。纯 JS 没有这个问题。
 *
 * @type {import('next').NextConfig}
 */
const nextConfig = {
  reactStrictMode: true,

  // 后端 API 反向代理，避免浏览器跨域；前端统一请求同源 /api/*。
  //
  // 注意：rewrites 在 `next build` 时求值并写进产物，所以 API_INTERNAL_URL
  // 必须在**构建时**传入，运行时改无效。
  async rewrites() {
    const api = process.env.API_INTERNAL_URL ?? "http://localhost:8080";
    return [{ source: "/api/:path*", destination: `${api}/api/:path*` }];
  },
};

export default nextConfig;
