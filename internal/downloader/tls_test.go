package downloader

import (
	"strconv"
	"testing"

	"github.com/EagleStelle/jasmr-dl/internal/util"
)

func TestUserAgentMatchesHandshake(t *testing.T) {
	if got := chromeHello.Version; got != strconv.Itoa(util.HandshakeMajor) {
		t.Fatalf("handshake imitates Chrome %s, util.HandshakeMajor says %d", got, util.HandshakeMajor)
	}

	if got := util.ChromeMajorVersion(util.UserAgent()); got != chromeHello.Version {
		t.Errorf("User-Agent claims Chrome %s, handshake sends Chrome %s", got, chromeHello.Version)
	}
}
