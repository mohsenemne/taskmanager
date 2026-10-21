package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/mohsenemne/taskmanager/internal/task"
	"github.com/mohsenemne/taskmanager/internal/task/mocks"
)

// withRecordingSpans installs an in-memory tracer provider for one test and
// returns the exporter holding the finished spans.
func withRecordingSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	return exporter
}

func spanAttributes(span tracetest.SpanStub) map[string]string {
	attrs := make(map[string]string, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = fmt.Sprintf("%v", attr.Value.AsInterface())
	}
	return attrs
}

func TestCreateRecordsItsOwnSpan(t *testing.T) {
	exporter := withRecordingSpans(t)

	id := uuid.New()
	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{ID: id, Title: "traced"}, nil)

	svc := NewTaskService(repo)
	_, err := svc.Create(context.Background(), task.Task{Title: "traced"})
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "task.create", spans[0].Name)
	require.Equal(t, id.String(), spanAttributes(spans[0])["task.id"])
}

func TestListRecordsFilterAndResultAttributes(t *testing.T) {
	exporter := withRecordingSpans(t)

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Return([]task.Task{{Title: "a"}}, 7, nil)

	svc := NewTaskService(repo)
	_, _, err := svc.List(context.Background(), task.Filter{Status: task.StatusDone, Limit: 5, Offset: 2})
	require.NoError(t, err)

	attrs := spanAttributes(exporter.GetSpans()[0])
	require.Equal(t, "done", attrs["filter.status"])
	require.Equal(t, "5", attrs["page.limit"])
	require.Equal(t, "2", attrs["page.offset"])
	require.Equal(t, "1", attrs["result.count"])
	require.Equal(t, "7", attrs["result.total"])
}

func TestValidationFailureIsRecordedWithoutFailingTheSpan(t *testing.T) {
	exporter := withRecordingSpans(t)

	svc := NewTaskService(mocks.NewMockRepository(t))
	_, err := svc.Create(context.Background(), task.Task{})
	require.ErrorIs(t, err, task.ErrInvalid)

	span := exporter.GetSpans()[0]
	require.Equal(t, codes.Unset, span.Status.Code, "a rejected input is not a system failure")
	require.NotEmpty(t, span.Events, "the error should still be recorded as an event")
}

func TestUnexpectedErrorFailsTheSpan(t *testing.T) {
	exporter := withRecordingSpans(t)

	repo := mocks.NewMockRepository(t)
	repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Return(nil, 0, errors.New("connection reset by peer"))

	svc := NewTaskService(repo)
	_, _, err := svc.List(context.Background(), task.Filter{})
	require.Error(t, err)

	span := exporter.GetSpans()[0]
	require.Equal(t, codes.Error, span.Status.Code)
}
