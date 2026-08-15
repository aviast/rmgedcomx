package loglevel

import (
	"log/slog"
	"testing"

	"github.com/go-openapi/testify/v2/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		value string
		want  slog.Level
	}{
		{"trace", Trace},
		{"TRACE", Trace},
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := Parse(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseRejectsUnknownLevel(t *testing.T) {
	_, err := Parse("verbose")
	require.Error(t, err)
}
