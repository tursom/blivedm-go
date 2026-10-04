package api

import "testing"

func TestExtractCookieValue(t *testing.T) {
	cookie := "SESSDATA=session; bili_jct=csrf; DedeUserID=12345; buvid3=buvid"

	value, ok := ExtractCookieValue(cookie, "bili_jct")
	if !ok || value != "csrf" {
		t.Fatalf("ExtractCookieValue() = %q, %v", value, ok)
	}
}

func TestExtractUIDFromCookie(t *testing.T) {
	uid, ok := ExtractUIDFromCookie("DedeUserID=12345; SESSDATA=session")
	if !ok || uid != 12345 {
		t.Fatalf("ExtractUIDFromCookie() = %d, %v", uid, ok)
	}
}

func TestExtractBuvidFromCookie(t *testing.T) {
	buvid, ok := ExtractBuvidFromCookie("buvid3=buvid3-value; _uuid=uuid-value")
	if !ok || buvid != "uuid-value" {
		t.Fatalf("ExtractBuvidFromCookie() = %q, %v", buvid, ok)
	}

	buvid, ok = ExtractBuvidFromCookie("buvid3=buvid3-value")
	if !ok || buvid != "buvid3-value" {
		t.Fatalf("ExtractBuvidFromCookie() fallback = %q, %v", buvid, ok)
	}
}
