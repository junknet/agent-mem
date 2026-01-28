package main

import (
	"net/http"
	"strings"
)

func requireToken(next http.Handler, token string) http.Handler {
	expected := strings.TrimSpace(token)
	if expected == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !matchToken(r, expected) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "未授权请求", "ERR_UNAUTHORIZED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func matchToken(r *http.Request, expected string) bool {
	if expected == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		if strings.TrimSpace(auth[7:]) == expected {
			return true
		}
	}
	if strings.TrimSpace(r.Header.Get("X-Agent-Mem-Token")) == expected {
		return true
	}
	if strings.TrimSpace(r.URL.Query().Get("token")) == expected {
		return true
	}
	return false
}
