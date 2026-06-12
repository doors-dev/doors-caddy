package handler

import (
	"net/http"
	"testing"
)

func TestTokenFromPath_WithRest(t *testing.T) {
	token, ok := tokenFromPath("/~/abc123/rest")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if token != "abc123" {
		t.Errorf("expected abc123, got %q", token)
	}
}

func TestTokenFromPath_NoRest(t *testing.T) {
	token, ok := tokenFromPath("/~/abc123")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if token != "abc123" {
		t.Errorf("expected abc123, got %q", token)
	}
}

func TestTokenFromPath_EmptyToken(t *testing.T) {
	_, ok := tokenFromPath("/~/")
	if ok {
		t.Error("expected ok=false for /~/ (len <= 3)")
	}
}

func TestTokenFromPath_SlashAfterPrefix(t *testing.T) {
	_, ok := tokenFromPath("/~//rest")
	if ok {
		t.Error("expected ok=false for /~//rest (empty token segment)")
	}
}

func TestTokenFromPath_NoPrefix(t *testing.T) {
	_, ok := tokenFromPath("/abc")
	if ok {
		t.Error("expected ok=false for /abc")
	}
}

func TestTokenFromPath_ShortPath(t *testing.T) {
	_, ok := tokenFromPath("/~")
	if ok {
		t.Error("expected ok=false for /~")
	}
}

func TestTokenFromCookie_Present(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "doors_upstream", Value: "some-token"})
	token, ok := tokenFromCookie("doors_upstream", r)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if token != "some-token" {
		t.Errorf("expected some-token, got %q", token)
	}
}

func TestTokenFromCookie_Absent(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	_, ok := tokenFromCookie("doors_upstream", r)
	if ok {
		t.Error("expected ok=false for absent cookie")
	}
}
