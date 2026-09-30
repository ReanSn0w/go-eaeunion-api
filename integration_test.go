package eaeunion

import (
	"context"
	"os"
	"testing"
)

// Run with EAEUNION_INTEGRATION=1. Endpoint and collection may be overridden.
func TestLiveCollection(t *testing.T) {
	if os.Getenv("EAEUNION_INTEGRATION") != "1" {
		t.Skip("live API check is opt-in")
	}
	endpoint := os.Getenv("EAEUNION_ENDPOINT")
	collection := os.Getenv("EAEUNION_COLLECTION")
	if endpoint == "" || collection == "" {
		t.Fatal("EAEUNION_ENDPOINT and EAEUNION_COLLECTION are required")
	}
	c, err := NewClient(Config{Endpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	page, err := c.Find(context.Background(), collection, Query{Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if page.ResponseSize > 1 {
		t.Fatalf("unexpected response size %d", page.ResponseSize)
	}
}
