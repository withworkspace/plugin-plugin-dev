# Build the plugin: ws-plugin-dev, which plugin.toml's command starts.
build:
    go build -o ws-plugin-dev .

# Its tests, against a stand-in for ws, then its app's. One builds a new
# plugin from the template, which needs npm and the network:
# `go test -short ./...` leaves it out.
test:
    go test ./...
    cd ui && npm test

check:
    gofmt -l . | grep . && exit 1 || true
    go vet ./...
    go build -o /dev/null .

# Build the app into ui/dist, which is committed: Go alone builds the
# plugin. It needs Node 24 and npm.
ui:
    cd ui && npm ci && npm run build

# Fail if the committed app isn't what its source builds.
ui-check: ui
    git diff --exit-code -- ui/dist
