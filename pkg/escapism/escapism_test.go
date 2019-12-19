package escapism

import (
	"testing"
)

func TestEscapeChar(t *testing.T) {
	safechar := escapeChar("@", "-")
	if safechar != "-40" {
		t.Error("Wrong escape char")
	}
}

func TestEscapeToDSLLabel(t *testing.T) {
	safestring := EscapeToDSLLabel("test@test.com")
	if safestring != "test-40test-2ecom" {
		t.Error("Wrong escape string")
	}
}
