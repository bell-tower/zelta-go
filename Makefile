# zelta-go Makefile
#
#   make            build bin/zelta and the bin/zprune link
#   make test       unit tests with the race detector
#   make cover      unit tests with a coverage report (coverage.html)
#   make lint       gofmt, go vet, staticcheck, and gopls diagnostics
#   make integration  end-to-end tests on temporary file-backed pools (root)
#   make shellspec  the Zelta ShellSpec compatibility suite, local and
#                   through fake remote hosts (root)
#   make install    install to PREFIX (DESTDIR supported)

PREFIX     ?= /usr/local
BINDIR     ?= $(PREFIX)/bin
MANDIR     ?= $(PREFIX)/man
ETCDIR     ?= $(PREFIX)/etc/zelta
SHAREDIR   ?= $(PREFIX)/share/zelta
GO         ?= go
VERSION    != (git describe --tags 2>/dev/null || echo 2.0.0-dev) | sed 's/^v//'
LDFLAGS    = -s -w -X github.com/bell-tower/zelta-go/internal/version.Version=$(VERSION)
GOFLAGS    ?= -trimpath
STATICCHECK ?= honnef.co/go/tools/cmd/staticcheck@2026.2.2
GOPLS      ?= golang.org/x/tools/gopls@v0.23.0

.PHONY: all build test cover lint vet fmt integration shellspec install uninstall clean completions

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/zelta ./cmd/zelta
	@ln -sf zelta bin/zprune

test:
	$(GO) test -race ./...

cover:
	$(GO) test -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html

vet:
	$(GO) vet ./...
	$(GO) vet -tags integration ./...

fmt:
	gofmt -w .

lint: vet
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt needed"; exit 1; }
	$(GO) run $(STATICCHECK) ./...
	$(GO) run $(STATICCHECK) -tags integration ./test/...
	@echo "gopls check"; out=$$(GOFLAGS=-tags=integration $(GO) run $(GOPLS) check -severity=hint $$(find . -name '*.go' -not -path './test/.shellspec-local/*')) && \
		test -z "$$out" || { echo "$$out"; exit 1; }

integration: build
	ZELTA_INTEGRATION=1 $(GO) test -tags integration -count=1 -v ./test/integration/...

shellspec: build
	./test/run_shellspec.sh standard
	./test/run_shellspec.sh remote

completions: build
	@mkdir -p build/completions
	bin/zelta completion bash > build/completions/zelta.bash
	bin/zelta completion zsh > build/completions/_zelta
	bin/zelta completion fish > build/completions/zelta.fish

install: build completions
	install -d $(DESTDIR)$(BINDIR) $(DESTDIR)$(MANDIR)/man7 $(DESTDIR)$(MANDIR)/man8 $(DESTDIR)$(ETCDIR) $(DESTDIR)$(SHAREDIR)/completions
	install -m 755 bin/zelta $(DESTDIR)$(BINDIR)/zelta
	ln -sf zelta $(DESTDIR)$(BINDIR)/zprune
	install -m 644 doc/man7/*.7 $(DESTDIR)$(MANDIR)/man7/
	install -m 644 doc/man8/*.8 $(DESTDIR)$(MANDIR)/man8/
	install -m 644 build/completions/* $(DESTDIR)$(SHAREDIR)/completions/
	install -m 644 zelta.env $(DESTDIR)$(ETCDIR)/zelta.env.example
	install -m 644 zelta.conf $(DESTDIR)$(ETCDIR)/zelta.conf.example

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/zelta $(DESTDIR)$(BINDIR)/zprune
	rm -f $(DESTDIR)$(MANDIR)/man7/zelta*.7 $(DESTDIR)$(MANDIR)/man8/zelta*.8 $(DESTDIR)$(MANDIR)/man8/zprune.8
	rm -rf $(DESTDIR)$(SHAREDIR)
	rm -f $(DESTDIR)$(ETCDIR)/zelta.env.example $(DESTDIR)$(ETCDIR)/zelta.conf.example

clean:
	rm -rf bin build coverage.out coverage.html
