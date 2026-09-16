//go:build !windows

// Linux'ta `go build ./...` yesil kalsin diye var: derleme etiketi tum
// dosyalari dislarsa paket "no Go files" hatasi verirdi. Calistirilirsa
// durumu acikca soyler.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "girginospanel-agent yalniz Windows icin derlenir; scripts/build-agent-windows.sh kullanin")
	os.Exit(2)
}
