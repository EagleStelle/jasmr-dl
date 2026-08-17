package downloader

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

const playlistBase = "https://v.example.xyz/RJ1.m3u8"

func TestParsePlaylistResolvesRelativeSegments(t *testing.T) {
	body := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:10",
		"#EXTINF:10.0,",
		"RJ1/seg_000000.ts",
		"#EXTINF:10.0,",
		"RJ1/seg_000001.ts",
		"#EXT-X-ENDLIST",
	}, "\n")

	segments, err := parsePlaylist(body, mustURL(t, playlistBase))
	if err != nil {
		t.Fatalf("parsePlaylist: %v", err)
	}

	want := []string{
		"https://v.example.xyz/RJ1/seg_000000.ts",
		"https://v.example.xyz/RJ1/seg_000001.ts",
	}
	if len(segments) != len(want) {
		t.Fatalf("got %d segments, want %d", len(segments), len(want))
	}
	for i, w := range want {
		if segments[i] != w {
			t.Errorf("segment %d = %q, want %q", i, segments[i], w)
		}
	}
}

func TestParsePlaylistRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "encrypted",
			body: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"k.key\"\n#EXTINF:10.0,\na.ts\n#EXT-X-ENDLIST",
			want: "encrypted",
		},
		{
			name: "live",
			body: "#EXTM3U\n#EXTINF:10.0,\na.ts",
			want: "live",
		},
		{
			name: "empty",
			body: "#EXTM3U\n#EXT-X-ENDLIST",
			want: "no segments",
		},
		{
			// A page-supplied playlist must not be able to aim segment
			// fetches at an unrelated host.
			name: "off-host segment",
			body: "#EXTM3U\n#EXTINF:10.0,\nhttps://evil.example.com/a.ts\n#EXT-X-ENDLIST",
			want: "off the playlist's host",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePlaylist(tc.body, mustURL(t, playlistBase))
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			var perm permanentError
			if !errors.As(err, &perm) {
				t.Error("want a permanent error, so retrying does not repeat it")
			}
		})
	}
}

func TestParsePlaylistAcceptsUnencryptedKey(t *testing.T) {
	body := "#EXTM3U\n#EXT-X-KEY:METHOD=NONE\n#EXTINF:10.0,\na.ts\n#EXT-X-ENDLIST"
	if _, err := parsePlaylist(body, mustURL(t, playlistBase)); err != nil {
		t.Fatalf("METHOD=NONE should be accepted: %v", err)
	}
}

func TestParsePlaylistCapsSegmentCount(t *testing.T) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for i := 0; i <= maxSegments; i++ {
		fmt.Fprintf(&b, "#EXTINF:10.0,\nseg%d.ts\n", i)
	}
	b.WriteString("#EXT-X-ENDLIST")

	_, err := parsePlaylist(b.String(), mustURL(t, playlistBase))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want a cap error, got %v", err)
	}
}

func TestFirstVariant(t *testing.T) {
	master := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-STREAM-INF:BANDWIDTH=64000",
		"low/index.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=128000",
		"high/index.m3u8",
	}, "\n")

	got, ok := firstVariant(master, mustURL(t, playlistBase))
	if !ok {
		t.Fatal("want a variant from a master playlist")
	}
	if want := "https://v.example.xyz/low/index.m3u8"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	media := "#EXTM3U\n#EXTINF:10.0,\na.ts\n#EXT-X-ENDLIST"
	if _, ok := firstVariant(media, mustURL(t, playlistBase)); ok {
		t.Error("a media playlist has no variants")
	}
}
