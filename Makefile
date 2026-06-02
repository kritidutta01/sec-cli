BINARY := bin/sec-cli
PKG    := ./...

.PHONY: build test lint fmt clean

build:
	go build -o $(BINARY) ./cmd/sec-cli

test:
	go test -race -count=1 $(PKG)

lint:
	golangci-lint run

fmt:
	gofumpt -w .

clean:
	rm -rf bin/
