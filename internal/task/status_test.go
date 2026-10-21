package task

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusValid(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{name: "todo", status: StatusTodo, want: true},
		{name: "in progress", status: StatusInProgress, want: true},
		{name: "done", status: StatusDone, want: true},
		{name: "empty", status: "", want: false},
		{name: "unknown", status: "blocked", want: false},
		{name: "wrong case", status: "TODO", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Valid())
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Status
		wantErr bool
	}{
		{name: "todo", raw: "todo", want: StatusTodo},
		{name: "in progress", raw: "in_progress", want: StatusInProgress},
		{name: "done", raw: "done", want: StatusDone},
		{name: "empty", raw: "", wantErr: true},
		{name: "unknown", raw: "blocked", wantErr: true},
		{name: "wrong case", raw: "Done", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatus(tt.raw)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalid)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
