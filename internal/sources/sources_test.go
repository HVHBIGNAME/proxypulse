package sources

import (
	"strconv"
	"testing"

	"github.com/HVHBIGNAME/proxypulse/internal/proxy"
)

func TestNormaliseAddr(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:8080":          "1.2.3.4:8080",
		"http://1.2.3.4:8080":   "1.2.3.4:8080",
		"socks5://1.2.3.4:1080": "1.2.3.4:1080",
		"https://1.2.3.4:9002":  "1.2.3.4:9002",
		"1.2.3.4:8080\tUS":      "1.2.3.4:8080",
		"  5.6.7.8:3128  ":      "5.6.7.8:3128",
		"# comment line":        "",
		"":                      "",
		"no-port-here":          "",
		"1.2.3.4:abc":           "",
	}
	for in, want := range cases {
		if got := normaliseAddr(in); got != want {
			t.Errorf("normaliseAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseIPPort(t *testing.T) {
	s := Source{ID: "t", Kind: "http", Format: "ipport"}
	got := parseIPPort([]byte("1.1.1.1:80\n2.2.2.2:3128\n\n# x\n3.3.3.3:1080\n"), s)
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
	if got[0].Addr != "1.1.1.1:80" || got[0].Kind != "http" || got[0].Source != "t" {
		t.Errorf("unexpected first candidate: %+v", got[0])
	}
}

func TestParseColon(t *testing.T) {
	s := Source{ID: "hideip", Kind: "http", Format: "colon"}
	got := parseColon([]byte("103.217.224.29:8089:Indonesia\n200.114.81.219:8080:Argentina\n"), s)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Addr != "103.217.224.29:8089" || got[0].Country != "Indonesia" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Country != "Argentina" {
		t.Errorf("second: %+v", got[1])
	}
}

func TestParseVakhov(t *testing.T) {
	s := Source{ID: "vakhov", Kind: "http", Format: "vakhov"}
	body := []byte(`[
	  {"ip":"72.195.101.99","port":"4145","country_code":"US","http":"0","ssl":"0","socks4":"1","socks5":"0"},
	  {"ip":"45.131.6.46","port":"80","country_code":"NL","http":"1","ssl":"0","socks4":"0","socks5":"0"}
	]`)
	got := parseVakhov(body, s)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Kind != "socks5" || got[0].Country != "US" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Kind != "http" || got[1].Country != "NL" {
		t.Errorf("second: %+v", got[1])
	}
}

func TestParseGeonode(t *testing.T) {
	s := Source{ID: "geonode", Kind: "http", Format: "geonode"}
	body := []byte(`{"data":[
	  {"ip":"10.0.0.1","port":"8080","country":"DE","protocols":["https"]},
	  {"ip":"10.0.0.2","port":"1080","country":"US","protocols":["socks5"]}
	]}`)
	got := parseGeonode(body, s)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Kind != "http" || got[0].Country != "DE" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Kind != "socks5" {
		t.Errorf("socks entry kind = %q, want socks5", got[1].Kind)
	}
}

func TestParseProxifly(t *testing.T) {
	s := Source{ID: "proxifly", Kind: "http", Format: "proxifly"}
	body := []byte(`[{"proxy":"10.0.0.9:4145","protocol":"socks5","geolocation":"US"}]`)
	got := parseProxifly(body, s)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Kind != "socks5" || got[0].Addr != "10.0.0.9:4145" {
		t.Errorf("unexpected: %+v", got[0])
	}
}

// TestCapSampleSpread checks the cap keeps a spread across the input, not a head slice.
func TestCapSampleSpread(t *testing.T) {
	in := make([]proxy.Candidate, 1000)
	for i := range in {
		in[i] = proxy.Candidate{Addr: "10.0.0.1:" + strconv.Itoa(i+1), Kind: "http"}
	}
	out := capSample(in, 10)
	if len(out) != 10 {
		t.Fatalf("got %d, want 10", len(out))
	}
	// The cap must not simply take the head.
	if out[9].Addr == in[9].Addr {
		t.Errorf("cap only took the head: %+v", out)
	}
	if len(capSample(in, 5000)) != 1000 {
		t.Errorf("cap must not pad when input is smaller than the cap")
	}
}

func TestRegistryKindsAreValid(t *testing.T) {
	reg := Registry()
	if len(reg) < 18 {
		t.Fatalf("registry has %d pools, expected the expanded set", len(reg))
	}
	seen := map[string]bool{}
	for _, s := range reg {
		if s.ID == "" || s.URL == "" {
			t.Errorf("source missing id/url: %+v", s)
		}
		if s.Kind != "http" && s.Kind != "socks5" {
			t.Errorf("source %s has invalid kind %q", s.ID, s.Kind)
		}
		if seen[s.ID] {
			t.Errorf("duplicate source id %q", s.ID)
		}
		seen[s.ID] = true
	}
}
