package caddybifrost

import (
	"fmt"
	"strings"
)

type SNIRoute struct {
	ServerName string `json:"server_name,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
}

type RouteTable struct {
	byServerName map[string]string
}

func NewRouteTable(routes []SNIRoute) (RouteTable, error) {
	table := RouteTable{byServerName: make(map[string]string, len(routes))}
	for index, route := range routes {
		serverName := normalizeServerName(route.ServerName)
		endpoint := strings.TrimSpace(route.Endpoint)
		if serverName == "" {
			return RouteTable{}, fmt.Errorf("routes[%d].server_name is required", index)
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

func (r RouteTable) Resolve(serverName string) (string, bool) {
	endpoint, ok := r.byServerName[normalizeServerName(serverName)]
	return endpoint, ok
}

func normalizeServerName(serverName string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(serverName), "."))
}
