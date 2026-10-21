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
