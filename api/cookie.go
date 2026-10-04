package api

import (
	"strconv"
	"strings"
)

func ExtractCookieValue(cookie, name string) (string, bool) {
	prefix := name + "="
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix), true
		}
	}
	return "", false
}

func ExtractUIDFromCookie(cookie string) (int, bool) {
	value, ok := ExtractCookieValue(cookie, "DedeUserID")
	if !ok {
		return 0, false
	}
	uid, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return uid, true
}

func ExtractBuvidFromCookie(cookie string) (string, bool) {
	if value, ok := ExtractCookieValue(cookie, "_uuid"); ok {
		return value, true
	}
	return ExtractCookieValue(cookie, "buvid3")
}
