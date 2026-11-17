# Build the plugin: ws-plugin-dev, which plugin.toml's command starts.
build:
    go build -o ws-plugin-dev .

# Its tests, against a stand-in for ws. One builds a new plugin from the
# template, which needs the network: `go test -short ./...` leaves it out.
test:
    go test ./...

check:
    gofmt -l . | grep . && exit 1 || true
    go vet ./...
    go build -o /dev/null .
