# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN npm install --global "$(node --print 'require("./package.json").packageManager')" \
    && pnpm install --frozen-lockfile
COPY web/index.html web/tsconfig.json web/vite.config.ts ./
COPY web/src ./src
RUN pnpm build

FROM golang:1.27.4-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -trimpath -o /out/rustdesk-hub ./cmd/rustdesk-hub

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -m 0700 -o 10001 -g 10001 /app/data
WORKDIR /app
COPY --from=backend /out/rustdesk-hub /usr/local/bin/rustdesk-hub
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/rustdesk-hub"]
