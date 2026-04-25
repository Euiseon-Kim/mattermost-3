PLUGIN_ID    ?= com.mattermost.gitlab-review
PLUGIN_VERSION ?= $(shell node -e "require('./plugin.json').version" 2>/dev/null || cat plugin.json | python3 -c 'import sys,json; print(json.load(sys.stdin)["version"])')
BUNDLE_NAME  ?= $(PLUGIN_ID)-$(PLUGIN_VERSION).tar.gz

PLATFORMS    ?= linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build server webapp bundle clean test check-style dist

## Default target
all: build

## Build everything
build: server webapp

## Build the server-side plugin binaries for all platforms
server:
	@echo "Building server plugin..."
	@mkdir -p server/dist
	@cd server && \
		GOOS=linux  GOARCH=amd64 go build -o dist/plugin-linux-amd64  . && \
		GOOS=darwin GOARCH=amd64 go build -o dist/plugin-darwin-amd64 . && \
		GOOS=darwin GOARCH=arm64 go build -o dist/plugin-darwin-arm64 . && \
		GOOS=windows GOARCH=amd64 go build -o dist/plugin-windows-amd64.exe .
	@echo "Server build complete."

## Build only linux/amd64 (faster for development)
server-linux:
	@echo "Building server plugin (linux/amd64)..."
	@mkdir -p server/dist
	cd server && GOOS=linux GOARCH=amd64 go build -o dist/plugin-linux-amd64 .

## Build the webapp
webapp:
	@echo "Building webapp..."
	@cd webapp && npm install && npm run build
	@echo "Webapp build complete."

## Bundle the plugin into a tar.gz
bundle: build
	@echo "Bundling plugin..."
	@rm -rf dist/bundle
	@mkdir -p dist/bundle/$(PLUGIN_ID)
	@cp plugin.json dist/bundle/$(PLUGIN_ID)/
	@cp -r assets dist/bundle/$(PLUGIN_ID)/
	@mkdir -p dist/bundle/$(PLUGIN_ID)/server/dist
	@cp server/dist/plugin-* dist/bundle/$(PLUGIN_ID)/server/dist/
	@mkdir -p dist/bundle/$(PLUGIN_ID)/webapp/dist
	@cp webapp/dist/main.js dist/bundle/$(PLUGIN_ID)/webapp/dist/
	@cd dist/bundle && tar -czf ../../$(BUNDLE_NAME) $(PLUGIN_ID)
	@rm -rf dist/bundle
	@echo "Bundle created: $(BUNDLE_NAME)"

## Package for deployment (alias for bundle)
dist: bundle

## Run server tests
test:
	cd server && go test ./...

## Run server linting
check-style:
	@cd server && go vet ./...

## Clean build artifacts
clean:
	rm -rf server/dist webapp/dist dist *.tar.gz

## Show help
help:
	@echo "GitLab Code Review Plugin - Build Targets"
	@echo ""
	@echo "  make build          - Build server and webapp"
	@echo "  make server         - Build server for all platforms"
	@echo "  make server-linux   - Build server for linux/amd64 only"
	@echo "  make webapp         - Build webapp"
	@echo "  make bundle         - Create installable .tar.gz bundle"
	@echo "  make test           - Run tests"
	@echo "  make clean          - Clean build artifacts"
