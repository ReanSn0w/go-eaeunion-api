package conformity

import (
	"context"
	"os"
	"testing"

	eaeunion "github.com/ReanSn0w/go-eaeunion-api"
)

func TestLiveRegistry(t *testing.T) {
	if os.Getenv("EAEUNION_INTEGRATION") != "1" {
		t.Skip("live registry check is opt-in")
	}
	c, err := NewPublicClient()
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	page, err := c.Find(context.Background(), Filter{}, eaeunion.Query{Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) > 1 {
		t.Fatalf("unexpected page size %d", len(page.Records))
	}
	if len(page.Records) == 1 && (page.Records[0].AuthorityID == "" || page.Records[0].Country.Value == "") {
		t.Fatalf("required fields missing: %+v", page.Records[0])
	}
}
