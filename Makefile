.PHONY: test build vet fmt tidy

PREFIX ?= .
BINDIR ?= $(PREFIX)/bin

test:
	go test ./...

shelltest: build
	ZELTA_BIN=./bin/zelta sh test/shell/basic_test.sh

vet:
	go vet ./...

fmt:
	gofmt -w .

build:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/zelta ./cmd/zelta
	cp $(BINDIR)/zelta $(BINDIR)/zprune

tidy:
	go mod tidy
