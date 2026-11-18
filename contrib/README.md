# contrib

Extras that ship with the repository but not with Zelta. `make install` and
`go install zelta.space/cmd/...` never build or install anything here.

Each Go tool is its own module (its own `go.mod`), so its dependencies stay
out of the `zelta.space` module that Sylve and other library users import.
Install one by its path:

```sh
go install zelta.space/contrib/zplan@latest
```

A tool's `go.work` points at the checkout's `zelta.space` module, so
building inside `contrib/<tool>` uses the library as it is in the tree.
`go install …@version` ignores `go.work` and uses the version in the
tool's `go.mod`.

Non-Go contributions sit alongside the Go tools with their own README.

| Tool | What |
|------|------|
| [`zplan`](zplan) | Interactive `zelta prune` planner |
