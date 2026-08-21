.PHONY: build test lint fixture
build:
	go build -o bin/herdr-tokens ./cmd/herdr-tokens
test:
	go test -race ./...
lint:
	go vet ./... && test -z "$$(gofmt -l .)"
fixture:
	./scripts/capture-fixture.sh testdata/snapshot.json
