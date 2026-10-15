# 多阶段构建：先构建站点前端与 Go 二进制，再产出最小运行镜像。
# 站点产物经 go:embed 打进二进制，运行镜像只需二进制本身。
FROM node:24-bookworm AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# 构建期仅产出静态站点，接口地址由运行时同源决定（前端取 window.location.origin）。
RUN BUILD_OUT_DIR=dist npm run build

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/blog-go ./cmd/blog-go

FROM debian:bookworm-slim
RUN useradd --system --create-home --home-dir /var/lib/blog-go blog
USER blog
WORKDIR /var/lib/blog-go
COPY --from=build /out/blog-go /usr/local/bin/blog-go
ENV HOST=127.0.0.1 PORT=19255 DB_DRIVER=sqlite DB_PATH=/var/lib/blog-go/blog.db UPLOAD_DIR=/var/lib/blog-go/uploads
EXPOSE 19255
ENTRYPOINT ["/usr/local/bin/blog-go"]
