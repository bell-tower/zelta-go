.PHONY: test build vet fmt tidy

PREFIX ?= .
BINDIR ?= $(PREFIX)/bin

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

build:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/zelta ./cmd/zelta

tidy:
	go mod tidy
