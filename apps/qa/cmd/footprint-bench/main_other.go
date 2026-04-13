//go:build !windows

// footprint-bench is Windows-only. This stub compiles on other platforms
// so `go build ./...` from the qa workspace does not fail, but prints a
// clear error on invocation.
package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Fprintf(os.Stderr,
		"footprint-bench: unsupported platform %s/%s — this harness uses Win32\n"+
			"GetProcessTimes + GetProcessMemoryInfo and only runs on Windows.\n"+
			"Run it on the Windows CI host that also builds personel-agent.exe.\n",
		runtime.GOOS, runtime.GOARCH)
	os.Exit(2)
}
