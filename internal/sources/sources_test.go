package sources

import "testing"

func TestNormaliseAddr(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:8080":          "1.2.3.4:8080",
		"http://1.2.3.4:8080":   "1.2.3.4:8080",
		"socks5://1.2.3.4:1080": "1.2.3.4:1080",
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

func TestRegistryKindsAreValid(t *testing.T) {
	for _, s := range Registry {
		if s.ID == "" || s.URL == "" {
			t.Errorf("source missing id/url: %+v", s)
		}
		if s.Kind != "http" && s.Kind != "socks5" {
			t.Errorf("source %s has invalid kind %q", s.ID, s.Kind)
		}
	}
}
