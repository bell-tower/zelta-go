//go:build !unix

package main

import "os"

func winch() <-chan os.Signal { return nil }
