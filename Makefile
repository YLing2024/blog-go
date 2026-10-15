BINARY := blog-go
GOFLAGS := -trimpath
LDFLAGS := -s -w
# 站点产物入口；构建后由根包 //go:embed all:web/dist 打进二进制。
# vite.config.ts 默认输出到生产站点目录，故这里显式用 BUILD_OUT_DIR 覆盖为仓库内 dist。
FRONTEND_DIST := web/dist/index.html
PKGS := . ./cmd/... ./internal/...

.PHONY: all web build test race vet fmt check clean docker run

all: build

# 前端产物（存在则跳过；`make clean` 或删除 web/dist 可强制重建）。
$(FRONTEND_DIST):
	cd web && timeout 600 npm install --no-audit --no-fund
	cd web && BUILD_OUT_DIR=dist timeout 600 npm run build

web: $(FRONTEND_DIST)

build: web
	CGO_ENABLED=0 timeout 600 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/blog-go

# vet / 单测都需要嵌入的前端产物目录存在。
vet: web
	timeout 300 go vet ./...

test: web
	timeout 600 go test $(PKGS) -count=1

race: web
	timeout 900 go test $(PKGS) -count=1 -race

# 聚合门禁：路由↔文档机检 + vet + 单测。退出码 0 才算过。
check: web
	node scripts/check-api-docs.mjs
	timeout 300 go vet ./...
	timeout 600 go test $(PKGS) -count=1

fmt:
	gofmt -l .

run: build
	./$(BINARY)

docker:
	docker build -t blog-go:latest .

clean:
	rm -f $(BINARY)
	rm -rf web/dist
