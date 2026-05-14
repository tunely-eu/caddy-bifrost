package config

import "testing"

func TestRouteTableResolveNormalizesServerNames(t *testing.T) {
	table, err := NewRouteTable([]SNIRoute{{ServerName: "Home.Example.Com.", Endpoint: "home"}})
	if err != nil {
		t.Fatalf("NewRouteTable: %v", err)
	}
	endpoint, ok := table.Resolve("home.example.com")
	if !ok {
		t.Fatal("expected route")
	}
	if endpoint != "home" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestRouteTableRejectsDuplicates(t *testing.T) {
	_, err := NewRouteTable([]SNIRoute{
		{ServerName: "home.example.com", Endpoint: "home"},
		{ServerName: "HOME.EXAMPLE.COM.", Endpoint: "other"},
	})
	if err == nil {
		t.Fatal("expected duplicate route error")
	}
}
