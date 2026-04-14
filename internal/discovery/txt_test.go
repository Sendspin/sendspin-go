// ABOUTME: Tests for TXT record parsing
// ABOUTME: Pure parser, no mDNS dependency

package discovery

import (
	"reflect"
	"testing"
)

func TestParseTXT(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   map[string]string
	}{
		{
			name:   "empty",
			fields: nil,
			want:   map[string]string{},
		},
		{
			name:   "single key",
			fields: []string{"path=/sendspin"},
			want:   map[string]string{"path": "/sendspin"},
		},
		{
			name:   "multiple keys",
			fields: []string{"path=/sendspin", "name=Living Room"},
			want:   map[string]string{"path": "/sendspin", "name": "Living Room"},
		},
		{
			name:   "key without value",
			fields: []string{"flag"},
			want:   map[string]string{"flag": ""},
		},
		{
			name:   "value contains equals",
			fields: []string{"token=abc=def=ghi"},
			want:   map[string]string{"token": "abc=def=ghi"},
		},
		{
			name:   "empty string ignored",
			fields: []string{"", "path=/sendspin"},
			want:   map[string]string{"path": "/sendspin"},
		},
		{
			name:   "duplicate key: last wins",
			fields: []string{"path=/old", "path=/new"},
			want:   map[string]string{"path": "/new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTXT(tt.fields)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTXT(%v) = %v, want %v", tt.fields, got, tt.want)
			}
		})
	}
}
