package config

import (
	"fmt"
	"strings"
)

// RouteTable resolves exact TLS SNI names to Bifrost endpoint keys.
type RouteTable struct {
	byServerName map[string]string
}

// SNIRoute maps one exact ClientHello SNI name to a Bifrost endpoint.
type SNIRoute struct {
	// ServerName is the exact SNI name to match. Wildcards are not supported.
	ServerName string `json:"server_name,omitempty"`

	// Endpoint is the Bifrost endpoint key that receives matching raw TLS
	// streams.
	Endpoint string `json:"endpoint,omitempty"`
}

// NewRouteTable validates SNI routes and returns a lookup table.
func NewRouteTable(routes []SNIRoute) (RouteTable, error) {
	table := RouteTable{byServerName: make(map[string]string, len(routes))}
	for index, route := range routes {
		serverName := NormalizeServerName(route.ServerName)
		endpoint := strings.TrimSpace(route.Endpoint)
		if serverName == "" {
			return RouteTable{}, fmt.Errorf("routes[%d].server_name is required", index)
		}
		if strings.ContainsAny(serverName, "*{}") {
			return RouteTable{}, fmt.Errorf("routes[%d].server_name must be an exact SNI name", index)
		}
		if endpoint == "" {
			return RouteTable{}, fmt.Errorf("routes[%d].endpoint is required", index)
		}
		if _, exists := table.byServerName[serverName]; exists {
			return RouteTable{}, fmt.Errorf("routes[%d].server_name duplicates an earlier route", index)
		}
		table.byServerName[serverName] = endpoint
	}
	return table, nil
}

// Resolve returns the endpoint configured for serverName.
func (r RouteTable) Resolve(serverName string) (string, bool) {
	endpoint, ok := r.byServerName[NormalizeServerName(serverName)]
	return endpoint, ok
}

// NormalizeServerName lowercases serverName, trims spaces, and removes a final
// DNS root dot.
func NormalizeServerName(serverName string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(serverName), "."))
}
