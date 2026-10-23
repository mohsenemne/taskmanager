package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// cursorVersion lets the encoding change without silently misreading cursors
// issued by an older build.
const cursorVersion = 1

// cursorPayload is the decoded form of a page cursor. It carries the ordering
// key only: the filter is applied server-side from the query string, so a
// cursor cannot widen or narrow what the caller is allowed to see.
type cursorPayload struct {
	Version int       `json:"v"`
	Created time.Time `json:"ts"`
	ID      uuid.UUID `json:"id"`
}

// encodeCursor renders a position as an opaque token.
func encodeCursor(c task.Cursor) string {
	payload, err := json.Marshal(cursorPayload{
		Version: cursorVersion,
		Created: c.CreatedAt,
		ID:      c.ID,
	})
	if err != nil {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(payload)
}

// decodeCursor parses a token produced by encodeCursor. An empty string means
// "start from the newest task".
func decodeCursor(raw string) (*task.Cursor, error) {
	if raw == "" {
		return nil, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}

	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, err
	}

	if payload.Version != cursorVersion {
		return nil, errors.New("unsupported cursor version")
	}

	if payload.Created.IsZero() || payload.ID == uuid.Nil {
		return nil, errors.New("incomplete cursor")
	}

	return &task.Cursor{CreatedAt: payload.Created, ID: payload.ID}, nil
}
