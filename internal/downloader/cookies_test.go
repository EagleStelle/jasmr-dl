package downloader

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func writeJar(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/cookies.txt"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadJar(t *testing.T, path string) http.CookieJar {
	t.Helper()
	cookies, _, err := LoadCookies(path)
	if err != nil {
		t.Fatal(err)
	}
	jar, err := NewJar(cookies)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestLoadCookies(t *testing.T) {
	jar := loadJar(t, writeJar(t, strings.Join([]string{
		"# Netscape HTTP Cookie File",
		"",
		"#HttpOnly_.japaneseasmr.com\tTRUE\t/\tTRUE\t2000000000\tcf_clearance\tabc.def-ghi",
		"japaneseasmr.com\tFALSE\t/\tFALSE\t0\tsession\t",
		".other.example\tTRUE\t/\tTRUE\t2000000000\telsewhere\tnope",
	}, "\n")))

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

// A browser export has no User-Agent line, and must still load.
func TestLoadCookiesUserAgent(t *testing.T) {
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/141.0.0.0 Safari/537.36"
	line := "japaneseasmr.com\tTRUE\t/\tTRUE\t0\tcf_clearance\tv\n"

	for name, body := range map[string]string{
		"recorded": uaPrefix + ua + "\n" + line,
		"absent":   line,
	} {
		_, got, err := LoadCookies(writeJar(t, body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := ua
		if name == "absent" {
			want = ""
		}
		if got != want {
			t.Errorf("%s: user-agent = %q, want %q", name, got, want)
		}
	}
}

func TestSaveCookiesRoundTrip(t *testing.T) {
	const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/141.0.0.0 Safari/537.36"
	want := []Cookie{
		{
			Domain:   ".japaneseasmr.com",
			Name:     "cf_clearance",
			Value:    "abc.def-ghi",
			Path:     "/",
			Expires:  time.Unix(2000000000, 0),
			Secure:   true,
			HTTPOnly: true,
		},
		{Domain: "japaneseasmr.com", Name: "pvc_visits[0]", Value: "", Path: "/"},
	}

	path := t.TempDir() + "/cookies.txt"
	if err := SaveCookies(path, ua, want); err != nil {
		t.Fatal(err)
	}

	got, gotUA, err := LoadCookies(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != ua {
		t.Errorf("user-agent = %q, want %q", gotUA, ua)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d cookies, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if !g.Expires.Equal(w.Expires) {
			t.Errorf("%s: expires = %v, want %v", w.Name, g.Expires, w.Expires)
		}
		g.Expires, w.Expires = time.Time{}, time.Time{}
		if g != w {
			t.Errorf("cookie %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestLoadCookiesRejectsJunk(t *testing.T) {
	for name, body := range map[string]string{
		"too few fields": "japaneseasmr.com\tTRUE\t/\tTRUE\tcf_clearance\n",
		"bad expiry":     "japaneseasmr.com\tTRUE\t/\tTRUE\tsoon\tcf_clearance\tv\n",
		"no name":        "japaneseasmr.com\tTRUE\t/\tTRUE\t0\t\tv\n",
		"only comments":  "# Netscape HTTP Cookie File\n\n",
	} {
		if _, _, err := LoadCookies(writeJar(t, body)); err == nil {
			t.Errorf("%s: got no error", name)
		}
	}
}
