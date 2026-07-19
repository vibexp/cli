package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestFromError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil is OK", nil, OK},
		{"plain error is runtime", errors.New("boom"), RuntimeErr},
		{"usage coded", New(UsageErr, errors.New("bad flag")), UsageErr},
		{"auth coded", Auth("unauthorized"), AuthErr},
		{"runtime coded", New(RuntimeErr, errors.New("api")), RuntimeErr},
		{"wrapped coded", fmt.Errorf("context: %w", Auth("nope")), AuthErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromError(tc.err); got != tc.want {
				t.Errorf("FromError = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCodedErrorUnwrap(t *testing.T) {
	base := errors.New("root cause")
	coded := New(AuthErr, base)
	if !errors.Is(coded, base) {
		t.Error("errors.Is should find wrapped base error")
	}
	if coded.Error() != "root cause" {
		t.Errorf("Error() = %q, want root cause", coded.Error())
	}
}

func TestCodedErrorNilInner(t *testing.T) {
	coded := &CodedError{Code: UsageErr}
	if coded.Error() == "" {
		t.Error("Error() should be non-empty even with nil inner error")
	}
}
