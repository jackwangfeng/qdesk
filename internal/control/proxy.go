package control

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// proxyToAgentd builds a single-shot reverse proxy aimed at the agentd
// listening on 127.0.0.1:hostPort.
//
// We strip the /v1/sessions/:id prefix before forwarding so the agentd sees
// /screenshot or /actions on its end.
func proxyToAgentd(hostPort int, stripPrefix string) http.Handler {
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hostPort))
	rp := httputil.NewSingleHostReverseProxy(target)
	original := rp.Director
	rp.Director = func(req *http.Request) {
		original(req)
		req.URL.Path = stripPathPrefix(req.URL.Path, stripPrefix)
		req.URL.RawPath = ""
		// Avoid surfacing the host's bearer token to the agentd.
		req.Header.Del("Authorization")
	}
	return rp
}

func stripPathPrefix(p, prefix string) string {
	if len(p) < len(prefix) || p[:len(prefix)] != prefix {
		return p
	}
	out := p[len(prefix):]
	if out == "" {
		out = "/"
	}
	return out
}
