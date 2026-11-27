package main

// The release's web client must be self-contained: local CanvasKit and fonts,
// no Google CDN, and a version.json the service worker agrees with.
// google_fonts, which the shared widgets use too, finds the bundled Geist
// files by name.

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	runtimeFetchingOff = regexp.MustCompile(`GoogleFonts\.config\.allowRuntimeFetching\s*=\s*false`)
	buildConfig        = regexp.MustCompile(`_flutter\.buildConfig\s*=\s*(\{[^;]+\});`)
	versionHash        = regexp.MustCompile(`["']version\.json["']\s*:\s*["']([a-f0-9]{32})["']`)
)

func checkWebBundle(root, web string) error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			fail("%v", err)
		}
		return string(raw)
	}

	entry := filepath.Join(root, "flutter_app", "lib", "main.dart")
	if !runtimeFetchingOff.MatchString(read(entry)) {
		fail("Google Fonts runtime fetching is not disabled")
	}

	bootstrap := read(filepath.Join(web, "flutter_bootstrap.js"))
	var config struct {
		UseLocalCanvasKit bool `json:"useLocalCanvasKit"`
	}
	if match := buildConfig.FindStringSubmatch(bootstrap); match == nil || json.Unmarshal([]byte(match[1]), &config) != nil {
		fail("flutter_bootstrap.js has no parseable Flutter build config")
	} else if !config.UseLocalCanvasKit {
		fail("Flutter is not configured to use local CanvasKit")
	}
	if index := read(filepath.Join(web, "index.html")); strings.Contains(index+bootstrap, "fonts.gstatic.com") || strings.Contains(index+bootstrap, "fonts.googleapis.com") {
		fail("web entry documents emit a remote Google Fonts CDN URL")
	}

	required := []string{
		"main.dart.js",
		"canvaskit/canvaskit.js", "canvaskit/canvaskit.wasm",
		"canvaskit/chromium/canvaskit.js", "canvaskit/chromium/canvaskit.wasm",
	}
	for _, face := range []string{
		"Geist-Light", "Geist-Regular", "Geist-Medium", "Geist-SemiBold", "Geist-Bold", "Geist-ExtraBold",
		"GeistMono-Regular", "GeistMono-Medium", "GeistMono-SemiBold", "GeistMono-Bold",
	} {
		required = append(required, "assets/assets/fonts/geist/"+face+".ttf")
	}
	for _, asset := range required {
		if info, err := os.Stat(filepath.Join(web, asset)); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			fail("missing asset %s", asset)
		}
	}

	hash := md5.Sum([]byte(read(filepath.Join(web, "version.json"))))
	match := versionHash.FindStringSubmatch(read(filepath.Join(web, "flutter_service_worker.js")))
	if match == nil {
		fail("flutter_service_worker.js does not cache version.json")
	} else if match[1] != hex.EncodeToString(hash[:]) {
		fail("version.json does not match the service worker resource hash %s", match[1])
	}
	if baked := regexp.MustCompile(`localhost:3344|127\.0\.0\.1:3344`); baked.MatchString(read(filepath.Join(web, "main.dart.js"))) {
		fail("the web client still points at a local dev server")
	}
	return errors.Join(problems...)
}
