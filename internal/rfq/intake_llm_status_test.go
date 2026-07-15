package rfq

import (
	"strings"
	"testing"
)

func TestHTTPStatusError_IncludesCodeAndSnippet(t *testing.T) {
	e := &httpStatusError{code: 404, snippet: `{"error":{"message":"Model not exist."}}`}
	got := e.Error()
	if !strings.Contains(got, "404") || !strings.Contains(got, "Model not exist.") {
		t.Fatalf("error missing code/snippet: %q", got)
	}
}

func TestHTTPStatusError_NoSnippetStillHasCode(t *testing.T) {
	e := &httpStatusError{code: 500}
	if !strings.Contains(e.Error(), "500") {
		t.Fatalf("error missing code: %q", e.Error())
	}
}
