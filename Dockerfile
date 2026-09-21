FROM node:26-alpine AS web
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --filter focusflow-web... --frozen-lockfile
COPY apps/web apps/web
RUN pnpm --dir apps/web build

FROM golang:1.26-alpine AS api
WORKDIR /src
COPY services/api/go.mod services/api/go.sum* ./
RUN go mod download
COPY services/api .
RUN CGO_ENABLED=0 go build -o /focusflow ./cmd/server

FROM alpine:3.24 AS runtime
RUN adduser -D -H focusflow
WORKDIR /app
COPY --from=api /focusflow /app/focusflow
COPY --from=web /src/apps/web/dist /app/web
USER focusflow
EXPOSE 8080
ENTRYPOINT ["/app/focusflow"]
