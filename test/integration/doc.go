// Package integration runs the zelta binary end to end against temporary
// file-backed ZFS pools. It requires root, OpenZFS, and ZELTA_INTEGRATION=1:
//
//	sudo ZELTA_INTEGRATION=1 go test -tags integration ./test/integration/
//
// Remote endpoints are exercised through a fake ssh that runs commands
// locally with sh, so every quoting layer of remote execution is real.
// With ZELTA_AWK_ROOT pointing at a checkout of the Awk implementation, the
// differential tests compare both implementations' output.
package integration
