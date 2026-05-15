package caddybifrost

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestListenerWrapperUnmarshalCaddyfileRoutes(t *testing.T) {
	d := caddyfile.NewTestDispenser(`bifrost {
	app bifrost
	route_sni Home.Example.Com. home
	route_sni files.example.com home
}`)
	var wrapper ListenerWrapper
	if err := wrapper.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if wrapper.App != "bifrost" {
		t.Fatalf("app = %q", wrapper.App)
	}
	if len(wrapper.Routes) != 2 {
		t.Fatalf("routes = %#v", wrapper.Routes)
	}
	if wrapper.Routes[0].ServerName != "Home.Example.Com." || wrapper.Routes[0].Endpoint != "home" {
		t.Fatalf("route = %#v", wrapper.Routes[0])
	}
}

func TestListenerWrapperRoutesDoNotCreateHTTPHostRoute(t *testing.T) {
	httpApp := provisionHTTPApp(t, `{
	local_certs
	servers :443 {
		listener_wrappers {
			bifrost {
				route_sni home.example.com home
			}
			tls
		}
	}
	bifrost {
		server public.example.com {
			endpoint home {
				token secret
			}
		}
	}
}

media.example.com {
	reverse_proxy home {
		transport bifrost
	}
}`)

	hosts := collectHTTPRouteHosts(httpApp)
	if !containsHost(hosts, "media.example.com") {
		t.Fatalf("expected public HTTP route host, got %#v", hosts)
	}
	if containsHost(hosts, "home.example.com") {
		t.Fatalf("private passthrough SNI leaked into HTTP routes: %#v", hosts)
	}
}

func provisionHTTPApp(t *testing.T, input string) *caddyhttp.App {
	t.Helper()
	configJSON := adaptCaddyfile(t, input)
	var cfg caddy.Config
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		t.Fatalf("unmarshal adapted config: %v", err)
	}
	ctx, err := caddy.ProvisionContext(&cfg)
	if err != nil {
		t.Fatalf("ProvisionContext: %v", err)
	}
	app, err := ctx.App("http")
	if err != nil {
		t.Fatalf("http app: %v", err)
	}
	httpApp, ok := app.(*caddyhttp.App)
	if !ok {
		t.Fatalf("http app type = %T", app)
	}
	return httpApp
}

func collectHTTPRouteHosts(httpApp *caddyhttp.App) []string {
	var hosts []string
	for _, server := range httpApp.Servers {
		if server == nil {
			continue
		}
		hosts = collectHTTPRouteListHosts(hosts, server.Routes)
	}
	return hosts
}

func collectHTTPRouteListHosts(hosts []string, routes caddyhttp.RouteList) []string {
	for _, route := range routes {
		for _, matcherSet := range route.MatcherSets {
			for _, matcher := range matcherSet {
				switch hostMatcher := matcher.(type) {
				case *caddyhttp.MatchHost:
					for _, host := range *hostMatcher {
						hosts = append(hosts, strings.ToLower(host))
					}
				case caddyhttp.MatchHost:
					for _, host := range hostMatcher {
						hosts = append(hosts, strings.ToLower(host))
					}
				}
			}
		}
		for _, handler := range route.Handlers {
			if subroute, ok := handler.(*caddyhttp.Subroute); ok {
				hosts = collectHTTPRouteListHosts(hosts, subroute.Routes)
			}
		}
	}
	return hosts
}

func containsHost(hosts []string, target string) bool {
	target = strings.ToLower(target)
	for _, host := range hosts {
		if strings.TrimSuffix(host, ".") == strings.TrimSuffix(target, ".") {
			return true
		}
	}
	return false
}
