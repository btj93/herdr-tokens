.PHONY: build test lint fixture install-hooks
build:
	go build -o bin/herdr-tokens ./cmd/herdr-tokens
test:
	go test -race ./...
lint:
	go vet ./... && test -z "$$(gofmt -l .)"
fixture:
	./scripts/capture-fixture.sh testdata/snapshot.json
install-hooks:
	git config core.hooksPath .githooks
