package settings

import (
	"errors"
	"testing"
	"time"
)

func TestToTestResult_Success(t *testing.T) {
	r := toTestResult("qwen3.7-plus", 120*time.Millisecond, nil)
	if !r.Ok || r.Model != "qwen3.7-plus" || r.Error != "" || r.LatencyMs != 120 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestToTestResult_Failure(t *testing.T) {
	r := toTestResult("bad", 5*time.Millisecond, errors.New("openai chat 404: Model not exist."))
	if r.Ok || r.Error == "" || r.Model != "bad" {
		t.Fatalf("unexpected: %+v", r)
	}
}
