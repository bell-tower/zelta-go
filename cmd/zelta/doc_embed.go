package main

import "embed"

//go:embed doc/man8/*.8 doc/man7/*.7
var manPages embed.FS
