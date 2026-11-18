# zelta-go Makefile
#
#   make            build bin/zelta and bin/zprune
#   make zplan      build bin/zplan, the contrib prune planner (not installed)
#   make test       unit tests with the race detector
#   make cover      unit tests with a coverage report (coverage.html)
#   make lint       gofmt, go vet, staticcheck, and gopls diagnostics
#   make integration  end-to-end tests on temporary file-backed pools (root)
#   make shellspec  the Zelta ShellSpec compatibility suite, local and
#                   through fake remote hosts (root)
#   make install    install to PREFIX (DESTDIR supported); non-root
#                   without PREFIX or DESTDIR uses the install.sh user layout

PREFIX     ?= /usr/local
BINDIR     ?= $(PREFIX)/bin
MANDIR     ?= $(PREFIX)/man
ETCDIR     ?= $(PREFIX)/etc/zelta
SHAREDIR   ?= $(PREFIX)/share/zelta
GO         ?= go
VERSION    ?=
LDFLAGS    = -s -w
GOFLAGS    ?= -trimpath
STATICCHECK ?= honnef.co/go/tools/cmd/staticcheck@2026.2.2
GOPLS      ?= golang.org/x/tools/gopls@v0.23.0

.PHONY: all build zplan test cover lint vet fmt integration shellspec install uninstall install-prefix uninstall-prefix clean completions

all: build

build:
	@mkdir -p bin
	@v='$(VERSION)'; [ -n "$$v" ] || v=$$(git describe --tags 2>/dev/null || echo 1.3.0-dev); v=$${v#v}; \
	echo "building zelta $$v"; \
	for c in zelta zprune; do \
		CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) -X zelta.space/internal/version.Version=$$v" -o bin/$$c ./cmd/$$c || exit 1; \
	done

zplan:
	@mkdir -p bin
	cd contrib/zplan && CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ../../bin/zplan .

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

# Non-root installs without PREFIX or DESTDIR use install.sh's user layout
# (~/bin, ~/.local/share/zelta, ~/.config/zelta), matching Zelta Awk.
install: build completions
	@if [ -z "$(DESTDIR)" ] && [ "$(PREFIX)" = /usr/local ] && [ "$$(id -u)" -ne 0 ]; then \
		ZELTA_BINARY=bin/zelta ./install.sh; \
	else \
		$(MAKE) install-prefix; \
	fi

uninstall:
	@if [ -z "$(DESTDIR)" ] && [ "$(PREFIX)" = /usr/local ] && [ "$$(id -u)" -ne 0 ]; then \
		./uninstall.sh home env; \
	else \
		$(MAKE) uninstall-prefix; \
	fi

install-prefix:
	install -d $(DESTDIR)$(BINDIR) $(DESTDIR)$(MANDIR)/man7 $(DESTDIR)$(MANDIR)/man8 $(DESTDIR)$(ETCDIR) $(DESTDIR)$(SHAREDIR)/completions
	install -m 755 bin/zelta $(DESTDIR)$(BINDIR)/zelta
	rm -f $(DESTDIR)$(BINDIR)/zprune
	install -m 755 bin/zprune $(DESTDIR)$(BINDIR)/zprune
	install -m 644 doc/man7/*.7 $(DESTDIR)$(MANDIR)/man7/
	install -m 644 doc/man8/*.8 $(DESTDIR)$(MANDIR)/man8/
	install -m 644 build/completions/* $(DESTDIR)$(SHAREDIR)/completions/
	install -m 644 zelta.env $(DESTDIR)$(ETCDIR)/zelta.env.example
	install -m 644 zelta.conf $(DESTDIR)$(ETCDIR)/zelta.conf.example

uninstall-prefix:
	rm -f $(DESTDIR)$(BINDIR)/zelta $(DESTDIR)$(BINDIR)/zprune
	rm -f $(DESTDIR)$(MANDIR)/man7/zelta*.7 $(DESTDIR)$(MANDIR)/man8/zelta*.8 $(DESTDIR)$(MANDIR)/man8/zprune.8
	rm -rf $(DESTDIR)$(SHAREDIR)
	rm -f $(DESTDIR)$(ETCDIR)/zelta.env.example $(DESTDIR)$(ETCDIR)/zelta.conf.example

clean:
	rm -rf bin build coverage.out coverage.html
