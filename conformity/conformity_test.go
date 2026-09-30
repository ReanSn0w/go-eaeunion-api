package conformity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	eaeunion "github.com/ReanSn0w/go-eaeunion-api"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	core, err := eaeunion.NewClient(eaeunion.Config{Endpoint: srv.URL + "/spd/find"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(core)
	if err != nil {
		t.Fatal(err)
	}
	return c, srv.Close
}

func TestDecodeFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	p, e := decode(eaeunion.Page{Result: records, ResponseSize: 2})
	if e != nil {
		t.Fatal(e)
	}
	if p.Records[0].Country.Value != "BY" || p.Records[0].RecordStatus.Code.Value != "02" || p.Records[0].Certificate.Status.Code.Value != "04" {
		t.Errorf("separate statuses: %+v", p.Records[0])
	}
	if len(p.Records[0].AccreditationAreas) != 1 || len(p.Records[0].Body.Addresses) != 1 || !strings.Contains(string(p.Records[0].Raw), "futureField") {
		t.Error("nested or unknown data lost")
	}
	if p.Records[0].AccreditationAreas[0].Objects[0].Name != "Пример продукции" || p.Records[0].Body.Addresses[0].Full != "Пример адреса" {
		t.Error("nested model not decoded")
	}
	if p.Records[1].Certificate.ID != "" || p.Records[1].Country.CodeListID != "P.CLS.019" {
		t.Error("optional fields or country lost")
	}
}

func TestFilterComposition(t *testing.T) {
	c, close := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("collection") != Collection {
			t.Errorf("collection: %s", r.URL.RawQuery)
		}
		var filter map[string][]map[string]any
		if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
			t.Error(err)
		}
		conditions := filter["$and"]
		if len(conditions) != 2 {
			t.Errorf("conditions: %#v", filter)
		}
		named := conditions[0]["$and"].([]any)
		if len(named) != 6 {
			t.Errorf("named conditions: %d", len(named))
		}
		if named[0].(map[string]any)["conformityAuthorityId"] != "ID" {
			t.Error("ID missing")
		}
		if named[1].(map[string]any)["unifiedCountryCode.value"] != "BY" {
			t.Error("country missing")
		}
		for _, criterion := range named {
			m := criterion.(map[string]any)
			if v, ok := m["conformityAssessmentBodyDetails.businessEntityUnitName"]; ok {
				regexpValue := v.(map[string]any)["$regex"]
				if regexpValue != "a\\.b" && regexpValue != "a.*b" {
					t.Errorf("regex %v", regexpValue)
				}
			}
		}
		if conditions[1]["extra"] != "value" {
			t.Error("query filter overwritten")
		}
		fmt.Fprint(w, `{"result":[],"responseSize":0}`)
	})
	defer close()
	_, err := c.Find(context.Background(), Filter{AuthorityID: "ID", Country: "BY", NameContains: "a.b", NameRegex: "a.*b", RecordStatus: "02", AccreditationStatus: "04"}, eaeunion.Query{Filter: map[string]any{"extra": "value"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Find(context.Background(), Filter{Extra: []string{"invalid"}}, eaeunion.Query{}); err == nil {
		t.Error("accepted non-object extra filter")
	}
}

func TestGetByIDOutcomes(t *testing.T) {
	c, close := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var filter map[string]any
		json.NewDecoder(r.Body).Decode(&filter)
		id := ""
		if v, ok := filter["conformityAuthorityId"]; ok {
			id = v.(string)
		} else if and, ok := filter["$and"].([]any); ok {
			id = and[0].(map[string]any)["conformityAuthorityId"].(string)
		}
		switch id {
		case "none":
			fmt.Fprint(w, `{"result":[],"responseSize":0,"matchedDocuments":0}`)
		case "one":
			fmt.Fprint(w, `{"result":[{"conformityAuthorityId":"one","unifiedCountryCode":{"value":"RU"}}],"responseSize":1,"matchedDocuments":1}`)
		default:
			fmt.Fprint(w, `{"result":[{"conformityAuthorityId":"duplicate"},{"conformityAuthorityId":"duplicate"}],"responseSize":2,"matchedDocuments":2}`)
		}
	})
	defer close()
	if _, err := c.GetByID(context.Background(), "none", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("not found: %v", err)
	}
	record, err := c.GetByID(context.Background(), "one", "RU")
	if err != nil || record.AuthorityID != "one" {
		t.Errorf("one: %+v %v", record, err)
	}
	if _, err := c.GetByID(context.Background(), "duplicate", ""); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("ambiguous: %v", err)
	}
}

func TestTypedWalk(t *testing.T) {
	var calls int
	c, close := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"result":[{"conformityAuthorityId":"ID","accreditationAreaDetails":[{"x":1}]}],"responseSize":1}`)
	})
	defer close()
	result, err := c.Walk(context.Background(), Filter{Country: "BY"}, eaeunion.Query{}, eaeunion.WalkOptions{PageSize: 1}, func(page Page) (bool, error) {
		if page.Records[0].AuthorityID != "ID" || len(page.Records[0].AccreditationAreas) != 1 {
			t.Error("typed page")
		}
		return false, nil
	})
	if err != nil || calls != 1 || result.Records != 0 || result.NextSkip != 0 {
		t.Errorf("walk %+v %v calls=%d", result, err, calls)
	}
}
