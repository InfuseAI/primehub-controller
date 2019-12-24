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

func TestEscapeChar2(t *testing.T) {
	safechar := escapeChar("字", "-")
	if safechar != "-e5-ad-97" {
		t.Error("Wrong escape char")
	}
}

func TestEscapeToDNSLabel(t *testing.T) {
	safestring := EscapeToDNSLabel("test@test.com")
	if safestring != "test-40test-2ecom" {
		t.Error("Wrong escape string")
	}
}

func TestEscapeToDNSLabel2(t *testing.T) {
	safestring := EscapeToDNSLabel("abcd測a試c@.b中文ed")
	if safestring != "abcd-e6-b8-aca-e8-a9-a6c-40-2eb-e4-b8-ad-e6-96-87ed" {
		t.Error("Wrong escape string")
	}
}

func TestUnescapeDNSLabel(t *testing.T) {
	normalstring := UnescapeDNSLabel("abcd-e6-b8-aca-e8-a9-a6c-40-2eb-e4-b8-ad-e6-96-87ed")
	if normalstring != "abcd測a試c@.b中文ed" {
		t.Error("Wrong unescape string")
	}
}

func TestEscapeToPrimehubLabel(t *testing.T) {
	safestring := EscapeToPrimehubLabel("abcd測a試c@.b中文ed")
	if safestring != "escaped-abcd-e6-b8-aca-e8-a9-a6c-40-2eb-e4-b8-ad-e6-96-87ed" {
		t.Error("Wrong escape string")
	}
}

func TestUnescapePrimehubLabel(t *testing.T) {
	normalstring := UnescapePrimehubLabel("escaped-abcd-e6-b8-aca-e8-a9-a6c-40-2eb-e4-b8-ad-e6-96-87ed")
	if normalstring != "abcd測a試c@.b中文ed" {
		t.Error("Wrong unescape string")
	}
}
