package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubPinger struct{ err error }

func (p stubPinger) Ping(context.Context) error { return p.err }

func TestReady(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "database available", want: true},
		{name: "database unavailable", err: errors.New("offline"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(stubPinger{err: tt.err}, time.Second)
			_, got := service.Ready(context.Background())
			if got != tt.want {
				t.Fatalf("Ready() = %v, want %v", got, tt.want)
			}
		})
	}
}
