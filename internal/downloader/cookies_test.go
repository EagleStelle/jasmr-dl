package downloader

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func writeJar(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/cookies.txt"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCookieJar(t *testing.T) {
	jar, err := LoadCookieJar(writeJar(t, strings.Join([]string{
		"# Netscape HTTP Cookie File",
		"",
		"#HttpOnly_.japaneseasmr.com\tTRUE\t/\tTRUE\t2000000000\tcf_clearance\tabc.def-ghi",
		"japaneseasmr.com\tFALSE\t/\tFALSE\t0\tsession\t",
		".other.example\tTRUE\t/\tTRUE\t2000000000\telsewhere\tnope",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}

	got := jar.Cookies(&url.URL{Scheme: "https", Host: "japaneseasmr.com", Path: "/12345/"})
	names := map[string]string{}
	for _, c := range got {
		names[c.Name] = c.Value
	}
	if names["cf_clearance"] != "abc.def-ghi" {
		t.Errorf("cf_clearance = %q, want abc.def-ghi", names["cf_clearance"])
	}
	if v, ok := names["session"]; !ok || v != "" {
		t.Errorf("session = %q, %v; want empty and present", v, ok)
	}
	if _, ok := names["elsewhere"]; ok {
		t.Error("another domain's cookie leaked into the request")
	}

	// The leading dot is what lets the cookie reach a subdomain.
	sub := jar.Cookies(&url.URL{Scheme: "https", Host: "cdn.japaneseasmr.com", Path: "/"})
	if len(sub) != 1 || sub[0].Name != "cf_clearance" {
		t.Errorf("subdomain got %v, want only cf_clearance", sub)
	}
}

func TestLoadCookieJarRejectsJunk(t *testing.T) {
	for name, body := range map[string]string{
		"too few fields": "japaneseasmr.com\tTRUE\t/\tTRUE\tcf_clearance\n",
		"bad expiry":     "japaneseasmr.com\tTRUE\t/\tTRUE\tsoon\tcf_clearance\tv\n",
		"no name":        "japaneseasmr.com\tTRUE\t/\tTRUE\t0\t\tv\n",
		"only comments":  "# Netscape HTTP Cookie File\n\n",
	} {
		if _, err := LoadCookieJar(writeJar(t, body)); err == nil {
			t.Errorf("%s: got no error", name)
		}
	}
}
