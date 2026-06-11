package upstream

import (
	"net/http"
	"strings"
)

func tokenFromPath(path string) (string, bool) {
	if len(path) <= 3 || path[:3] != "/~/" {
		return "", false
	}
	rest := path[3:]
	i := strings.IndexByte(rest, '/')
	if i == -1 {
		return rest, true
	}
	if i == 0 {
		return "", false
	}
	return rest[:i], true
}

func tokenFromCookie(name string, r *http.Request) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil {
		return "", false
	}
	return c.Value, true
}
