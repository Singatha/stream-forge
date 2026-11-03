package video

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	original := Cursor{CreatedAt: time.Date(2026, 11, 3, 12, 30, 0, 123000000, time.UTC), ID: uuid.New()}
	encoded, err := EncodeCursor(original)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded.ID != original.ID || !decoded.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("decoded = %#v, want %#v", decoded, original)
	}
}

func TestCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"not-base64***", "e30", string(make([]byte, maxCursorLength+1))} {
		if _, err := DecodeCursor(value); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("DecodeCursor(%q) error = %v, want ErrInvalidCursor", value, err)
		}
	}
}
