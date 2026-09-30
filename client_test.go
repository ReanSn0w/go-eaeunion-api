package eaeunion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func integer(n int) *int   { return &n }
func boolean(v bool) *bool { return &v }

func TestFindRequestAndResponse(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "text/plain" || r.URL.Path != "/spd/find" {
			t.Errorf("request method/header/path: %s %s %s", r.Method, r.Header.Get("Content-Type"), r.URL.Path)
		}
		collection := r.URL.Query().Get("collection")
		if collection != "first.one" && collection != "other/collection" {
			t.Errorf("collection: %q", collection)
		}
		if r.URL.Query().Get("sort") != `{"country":1,"name":-1}` {
			t.Errorf("sort order: %q", r.URL.Query().Get("sort"))
		}
		if r.URL.Query().Get("fields") != `{"name":1,"secret":0}` || r.URL.Query().Get("skipCount") != "false" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		var filter map[string]any
		if err := json.NewDecoder(r.Body).Decode(&filter); err != nil || filter["name"] != "a+b" {
			t.Errorf("filter: %v %v", filter, err)
		}
		mu.Lock()
		seen[collection] = true
		mu.Unlock()
		fmt.Fprint(w, `{"result":[{"id":9007199254740993,"nested":{"list":[1,2]}}],"responseSize":1}`)
	}))
	defer srv.Close()
	c, err := NewClient(Config{Endpoint: srv.URL + "/spd/find"})
	if err != nil {
		t.Fatal(err)
	}
	q := Query{Filter: map[string]any{"name": "a+b"}, Sort: []SortField{{"country", 1}, {"name", -1}}, Fields: []Field{{"name", 1}, {"secret", 0}}, SkipCount: boolean(false)}
	var wg sync.WaitGroup
	for _, name := range []string{"first.one", "other/collection"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			page, e := c.Find(context.Background(), name, q)
			if e != nil {
				t.Error(e)
				return
			}
			if page.TotalDocuments != nil {
				t.Error("unexpected count")
			}
			items, e := DecodePage[map[string]any](page)
			if e != nil {
				t.Error(e)
				return
			}
			if items[0]["id"].(json.Number).String() != "9007199254740993" {
				t.Error("rounded ID")
			}
			if !strings.Contains(string(page.Result[0]), `"list":[1,2]`) {
				t.Error("nested JSON lost")
			}
		}(name)
	}
	wg.Wait()
	if len(seen) != 2 {
		t.Errorf("seen collections: %v", seen)
	}
}

func TestFindErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		max        int64
		want       string
	}{
		{"access", `Недостаточно прав`, 500, 0, "HTTP 500"},
		{"malformed", "not JSON", 200, 0, "decode API response"},
		{"bad envelope", `{"result":[],"responseSize":1}`, 200, 0, "inconsistent"},
		{"too large", strings.Repeat("x", 100), 200, 20, "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c, e := NewClient(Config{Endpoint: srv.URL + "/find", MaxResponseBytes: tc.max})
			if e != nil {
				t.Fatal(e)
			}
			_, e = c.Find(context.Background(), "anything", Query{})
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("error %v, want %q", e, tc.want)
			}
		})
	}
	for _, q := range []Query{{Limit: integer(0)}, {Skip: integer(-1)}, {Filter: json.RawMessage(`[]`)}, {Sort: []SortField{{"x", 0}}}} {
		if _, _, err := encodeQuery(q); err == nil {
			t.Fatalf("accepted invalid query %+v", q)
		}
	}
}

func TestOAuthCachingAndGateway(t *testing.T) {
	var mu sync.Mutex
	var tokenCalls, groupCalls, dataCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/token" {
			tokenCalls++
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("token content type")
			}
			r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "secret" {
				t.Error("OAuth form")
			}
			fmt.Fprint(w, `{"access_token":"access-secret","token_type":"Bearer","expires_in":3600}`)
			return
		}
		var payload gatewayRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.ID == "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("gateway envelope")
		}
		if payload.ServiceKey == "platform_svc" {
			groupCalls++
			if r.Header.Get("grant_type") != "refresh_token" || payload.Path != "/token/login_by_token" || payload.Headers[0].Value != "Bearer access-secret" {
				t.Error("group request")
			}
			fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_SUCCESS","responseStatus":200,"responseBody":"group-secret"}}`)
			return
		}
		dataCalls++
		if payload.ServiceKey != "spd" || !strings.HasPrefix(payload.Path, "/find?collection=a.collection") || payload.RequestType != "POST" || payload.Headers[0].Value != "Bearer group-secret" || payload.Body != "{}" {
			t.Errorf("data request: %+v", payload)
		}
		fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_SUCCESS","responseStatus":200,"responseBody":"{\"result\":[],\"responseSize\":0}"}}`)
	}))
	defer srv.Close()
	c, e := NewClient(Config{Endpoint: srv.URL + "/spd/find", Mode: Gateway, Credentials: &ClientCredentials{TokenURL: srv.URL + "/token", ClientID: "client", ClientSecret: "secret"}, Gateway: &GatewayConfig{URL: srv.URL + "/sendJson?sync=true", ServiceKey: "spd", CountryTo: "EEC"}})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := c.Find(context.Background(), "a.collection", Query{}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if tokenCalls != 1 || groupCalls != 1 || dataCalls != 8 {
		t.Errorf("calls: OAuth %d group %d data %d", tokenCalls, groupCalls, dataCalls)
	}
}

func TestGatewayInnerErrorAndRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p gatewayRequest
		json.NewDecoder(r.Body).Decode(&p)
		if p.ServiceKey == "platform_svc" {
			fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_SUCCESS","responseStatus":200,"responseBody":"group-secret"}}`)
			return
		}
		fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_ERROR","responseStatus":403,"responseBody":"Bearer group-secret denied"}}`)
	}))
	defer srv.Close()
	c, e := NewClient(Config{Endpoint: srv.URL + "/spd/find", Mode: Gateway, TokenProvider: StaticToken("access-secret"), Gateway: &GatewayConfig{URL: srv.URL + "/gateway", ServiceKey: "spd", CountryTo: "EEC"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Find(context.Background(), "collection", Query{})
	if e == nil || !strings.Contains(e.Error(), "403") || strings.Contains(e.Error(), "secret") {
		t.Fatalf("unsafe gateway error: %v", e)
	}
}

func TestGatewayRefreshesOnlyAfterUnauthorized(t *testing.T) {
	var groupCalls, dataCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p gatewayRequest
		json.NewDecoder(r.Body).Decode(&p)
		if p.ServiceKey == "platform_svc" {
			groupCalls++
			fmt.Fprintf(w, `{"status":"OK","request":{"status":"RESPONSE_SUCCESS","responseStatus":200,"responseBody":"group-%d"}}`, groupCalls)
			return
		}
		dataCalls++
		if p.Headers[0].Value == "Bearer group-1" {
			fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_ERROR","responseStatus":401}}`)
			return
		}
		fmt.Fprint(w, `{"status":"OK","request":{"status":"RESPONSE_SUCCESS","responseStatus":200,"responseBody":"{\"result\":[],\"responseSize\":0}"}}`)
	}))
	defer srv.Close()
	c, e := NewClient(Config{Endpoint: srv.URL + "/spd/find", Mode: Gateway, TokenProvider: StaticToken("access"), Gateway: &GatewayConfig{URL: srv.URL + "/gateway", ServiceKey: "spd", CountryTo: "EEC"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.Find(context.Background(), "x", Query{})
	if e != nil || groupCalls != 2 || dataCalls != 2 {
		t.Errorf("retry: %v, group=%d, data=%d", e, groupCalls, dataCalls)
	}
}

func TestOAuthExpirationAndErrorRedaction(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 3 {
			w.WriteHeader(400)
			fmt.Fprint(w, "client-secret rejected")
			return
		}
		fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":1}`, calls)
	}))
	defer srv.Close()
	p, e := newCredentialProvider(ClientCredentials{TokenURL: srv.URL, ClientID: "client", ClientSecret: "client-secret"}, srv.Client(), time.Second, 1024)
	if e != nil {
		t.Fatal(e)
	}
	first, e := p.Token(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	again, e := p.Token(context.Background())
	if e != nil || again != first || calls != 1 {
		t.Errorf("cache %q %q %d %v", first, again, calls, e)
	}
	time.Sleep(950 * time.Millisecond)
	second, e := p.Token(context.Background())
	if e != nil || second == first || calls != 2 {
		t.Errorf("refresh %q %d %v", second, calls, e)
	}
	time.Sleep(950 * time.Millisecond)
	_, e = p.Token(context.Background())
	if e == nil || strings.Contains(e.Error(), "client-secret") {
		t.Errorf("redaction: %v", e)
	}
}

func TestWalkAndCancellation(t *testing.T) {
	var skips []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		skips = append(skips, r.URL.Query().Get("skip"))
		skip := r.URL.Query().Get("skip")
		if skip == "4" {
			fmt.Fprint(w, `{"result":[{"id":5}],"responseSize":1}`)
		} else {
			fmt.Fprint(w, `{"result":[{"id":1},{"id":2}],"responseSize":2}`)
		}
	}))
	defer srv.Close()
	c, _ := NewClient(Config{Endpoint: srv.URL + "/find"})
	result, e := c.Walk(context.Background(), "x", Query{Skip: integer(2), SkipCount: boolean(true)}, WalkOptions{PageSize: 2}, func(p Page) (bool, error) { return true, nil })
	if e != nil {
		t.Fatal(e)
	}
	if result.NextSkip != 5 || result.Pages != 2 || result.Records != 3 || strings.Join(skips, ",") != "2,4" {
		t.Errorf("walk %+v skips %v", result, skips)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = c.Walk(ctx, "x", Query{}, WalkOptions{}, func(Page) (bool, error) { return true, nil })
	if e != context.Canceled {
		t.Errorf("cancel: %v", e)
	}
	stop, e := c.Walk(context.Background(), "x", Query{Skip: integer(2)}, WalkOptions{PageSize: 2}, func(Page) (bool, error) { return false, nil })
	if e != nil || stop.NextSkip != 2 || stop.Records != 0 {
		t.Errorf("stopped %+v %v", stop, e)
	}
}

func TestTimeoutAndRedirect(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer slow.Close()
	c, _ := NewClient(Config{Endpoint: slow.URL + "/find", Timeout: 10 * time.Millisecond})
	_, e := c.Find(context.Background(), "x", Query{})
	if e == nil {
		t.Fatal("timeout not enforced")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("secret forwarded to foreign origin") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c, _ = NewClient(Config{Endpoint: redirect.URL + "/find", Mode: DirectToken, TokenProvider: StaticToken("secret")})
	_, e = c.Find(context.Background(), "x", Query{})
	if e == nil {
		t.Fatal("redirect accepted")
	}
}
