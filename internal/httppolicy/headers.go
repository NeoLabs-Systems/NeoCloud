// Package httppolicy holds NeoCloud's request and response rules that apply to
// every route: security headers, request body limits and error redaction.
package httppolicy

import (
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/secheaders"
)

type HeaderOptions struct {
	SecureCookies bool
	HSTSMaxAge    int
	// AllowExternal admits the Flutter engine and fonts from Google's CDN.
	AllowExternal bool
	// ConnectOrigins are further hosts pages may send requests to.
	ConnectOrigins []string
	// EditorOrigins are read per request: the document editor is configurable
	// while the server runs.
	EditorOrigins func() []string
}

// SecurityHeaders applies the baseline headers and the content security policy.
// Handlers that serve untrusted or token-bearing content override them.
func SecurityHeaders(options HeaderOptions) func(http.Handler) http.Handler {
	config := secheaders.Config{}
	if options.SecureCookies {
		config.HSTSMaxAgeSeconds = options.HSTSMaxAge
	}
	base := secheaders.Headers(config)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			for name, values := range base {
				header[name] = values
			}
			header.Set("Content-Security-Policy", options.policy().String())
			// What a web client on another allowed origin may read of a download.
			if r.Header.Get("Origin") != "" {
				header.Set("Access-Control-Expose-Headers", "Content-Range,Accept-Ranges,Content-Length,ETag,Content-Disposition")
			}
			// Even a refused signing request must not let its token travel on.
			if strings.HasPrefix(r.URL.Path, "/api/public/sign") {
				header.Set("Referrer-Policy", "no-referrer")
				header.Set("X-Robots-Tag", "noindex, nofollow")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (o HeaderOptions) policy() secheaders.Policy {
	script := []string{"'self'", "'unsafe-inline'", "blob:", "'wasm-unsafe-eval'"}
	connect := []string{"'self'", "blob:", "data:"}
	if o.SecureCookies {
		connect = append(connect, "wss:")
	} else {
		connect = append(connect, "ws:", "wss:")
	}
	if o.AllowExternal {
		script = append(script, "https://www.gstatic.com")
		connect = append(connect, "https://fonts.googleapis.com", "https://fonts.gstatic.com", "https://www.gstatic.com")
	}
	connect = append(connect, o.ConnectOrigins...)
	var editors []string
	if o.EditorOrigins != nil {
		editors = o.EditorOrigins()
	}
	if len(editors) == 0 {
		editors = []string{"'self'"}
	}
	return secheaders.Policy{
		{Name: "default-src", Values: []string{"'self'"}},
		{Name: "script-src", Values: script},
		{Name: "script-src-attr", Values: []string{"'none'"}},
		{Name: "style-src", Values: []string{"'self'", "'unsafe-inline'", "https://fonts.googleapis.com"}},
		{Name: "img-src", Values: []string{"'self'", "data:", "blob:"}},
		{Name: "media-src", Values: []string{"'self'", "data:", "blob:"}},
		{Name: "connect-src", Values: connect},
		{Name: "font-src", Values: []string{"'self'", "data:", "https://fonts.gstatic.com"}},
		{Name: "worker-src", Values: []string{"'self'", "blob:"}},
		{Name: "frame-src", Values: append([]string{"'self'", "blob:"}, editors...)},
		{Name: "form-action", Values: append([]string{"'self'"}, editors...)},
		{Name: "frame-ancestors", Values: []string{"'self'"}},
		{Name: "base-uri", Values: []string{"'self'"}},
		{Name: "object-src", Values: []string{"'none'"}},
	}
}
