# 协同编辑服务（Hocuspocus + Yjs）。
# 依赖很少，直接把 workspace 里的 package.json 装上即可。
FROM node:22-alpine AS deps
RUN corepack enable && corepack prepare pnpm@11.1.3 --activate
WORKDIR /repo

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/ws/package.json apps/ws/package.json
COPY apps/web/package.json apps/web/package.json

RUN pnpm install --frozen-lockfile --filter @thesis/ws...

FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production

COPY --from=deps /repo/node_modules ./node_modules
COPY --from=deps /repo/apps/ws/node_modules ./apps/ws/node_modules
COPY apps/ws/package.json ./apps/ws/package.json
COPY apps/ws/src ./apps/ws/src

RUN addgroup -S app && adduser -S -G app app && chown -R app:app /app
USER app

EXPOSE 1234 1235
WORKDIR /app/apps/ws
CMD ["node", "src/index.js"]
