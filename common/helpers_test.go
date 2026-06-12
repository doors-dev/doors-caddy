package common

import (
	"errors"
	"testing"
)

func TestErrorsJoin(t *testing.T) {
	main := errors.New("main")
	specific := errors.New("specific")
	joined := ErrorsJoin(main, specific)

	if !errors.Is(joined, main) {
		t.Error("joined error should wrap main error")
	}
	if !errors.Is(joined, specific) {
		t.Error("joined error should wrap specific error")
	}
}

func TestErrorsJoin_Format(t *testing.T) {
	main := errors.New("main")
	specific := errors.New("specific")
	joined := ErrorsJoin(main, specific)

	expected := "main: specific"
	if joined.Error() != expected {
		t.Errorf("expected %q, got %q", expected, joined.Error())
	}
}
