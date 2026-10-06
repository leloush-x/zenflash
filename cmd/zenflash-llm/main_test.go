package main

import "testing"

func TestSameHTTPServiceMatchesEmbeddedClineLoopback(t *testing.T) {
	if !sameHTTPService("http://127.0.0.1:3457", "http://localhost:3457") {
		t.Fatal("loopback aliases on the same port should identify the embedded Cline server")
	}
	if sameHTTPService("http://127.0.0.1:3457", "http://127.0.0.1:3458") {
		t.Fatal("different ports must not identify the embedded Cline server")
	}
	if sameHTTPService("http://api.example:3457", "http://127.0.0.1:3457") {
		t.Fatal("external Go upstream must not be treated as the embedded Cline server")
	}
}
