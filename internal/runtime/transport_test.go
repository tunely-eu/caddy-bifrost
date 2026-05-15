package runtime

import "testing"

func TestEndpointFromDialAddress(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    string
	}{
		{address: "home:80", want: "home"},
		{address: "home:8123", want: "home"},
		{address: "home", want: "home"},
		{address: "[::1]:80", want: "::1"},
	} {
		got, err := endpointFromDialAddress(tc.address)
		if err != nil {
			t.Fatalf("endpointFromDialAddress(%q): %v", tc.address, err)
		}
		if got != tc.want {
			t.Fatalf("endpointFromDialAddress(%q) = %q, want %q", tc.address, got, tc.want)
		}
	}
}

func TestEndpointFromDialAddressRejectsEmptyAddress(t *testing.T) {
	if _, err := endpointFromDialAddress(""); err == nil {
		t.Fatal("expected empty address error")
	}
}
