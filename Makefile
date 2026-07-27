.PHONY: test build vet fmt tidy doc shelltest shellspec shellspec-standard

PREFIX ?= .
BINDIR ?= $(PREFIX)/bin

test:
	go test ./...

shelltest: build
	ZELTA_BIN=./bin/zelta sh test/shell/basic_test.sh

# ShellSpec: install + no-op CLI checks + cleanup (no ZFS pools required)
shellspec: build
	SANDBOX_ZELTA_TMP_SUFFIX=$${SANDBOX_ZELTA_TMP_SUFFIX:-$$$$} \
		shellspec --tag install,cleanup

# Full standard scenario (requires sudo ZFS; set SANDBOX_ZELTA_* pools/ds)
shellspec-standard: build
	./test/run_tests.sh standard

vet:
	go vet ./...

fmt:
	gofmt -w .

build:
	mkdir -p $(BINDIR)
	go build -o $(BINDIR)/zelta ./cmd/zelta
	cp $(BINDIR)/zelta $(BINDIR)/zprune

doc:
	rsync -a ../zelta-awk/doc/man8/ cmd/zelta/doc/man8/
	rsync -a ../zelta-awk/doc/man7/ cmd/zelta/doc/man7/
	mkdir -p doc
	rsync -a cmd/zelta/doc/man8/ doc/man8/
	rsync -a cmd/zelta/doc/man7/ doc/man7/

tidy:
	go mod tidy
