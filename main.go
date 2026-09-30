// Command tjek is a keyboard-driven terminal task manager. The app is in
// internal/app; this file is the entry point and the slot the build writes
// the version into.
package main

import "github.com/Iliorn/tjek/internal/app"

// appVersion is the build version, set by the release:
//
//	go build -ldflags "-X main.appVersion=v1.8.0" -o tjek .
var appVersion = "dev"

func main() {
	app.Main(appVersion)
}
