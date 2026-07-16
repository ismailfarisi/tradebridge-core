package notify

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestNewSender_DefaultsToLogWhenNoKey(t *testing.T) {
	s := NewSender("", "from@x.com", zerolog.Nop())
	_, ok := s.(LogSender)
	require.True(t, ok, "empty API key should yield LogSender")
	require.NoError(t, s.Send(context.Background(), Email{To: []string{"a@b.com"}, Subject: "hi"}))
}

func TestNewSender_ResendWhenKeySet(t *testing.T) {
	s := NewSender("re_123", "from@x.com", zerolog.Nop())
	_, ok := s.(*ResendSender)
	require.True(t, ok, "non-empty API key should yield ResendSender")
}
