package content

import (
	"reflect"
	"testing"
)

func TestPostTags(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		want    []string
		wantErr bool
	}{
		{name: "pgx text array", value: []any{"go", "research"}, want: []string{"go", "research"}},
		{name: "string slice", value: []string{"go"}, want: []string{"go"}},
		{name: "empty pgx array", value: []any{}, want: []string{}},
		{name: "invalid element", value: []any{1}, wantErr: true},
		{name: "null", value: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := postTags(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("postTags() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("postTags() = %v, want %v", got, tt.want)
			}
		})
	}
}
