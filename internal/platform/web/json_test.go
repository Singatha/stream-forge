package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid", body: `{"name":"forge"}`},
		{name: "unknown field", body: `{"name":"forge","admin":true}`, wantErr: true},
		{name: "multiple values", body: `{"name":"forge"}{"name":"other"}`, wantErr: true},
		{name: "empty", body: ``, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var destination struct {
				Name string `json:"name"`
			}
			err := DecodeJSON(response, request, &destination)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DecodeJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
