# Allocation-Free SpanFromContext GLS Fallback

## Problem

The base instrumentation rule wraps every OpenTelemetry
`trace.SpanFromContext` call in the generic exit trampoline. The trampoline
constructs a `CallContext`, parameter storage, return-value storage, and a
deferred hook even though the hook only conditionally replaces one return
value. In the production property-writer profile this path produced no retained
heap after GC, but generated continuous short-lived allocation churn.

## Design

Inject a small linkname declaration into `go.opentelemetry.io/otel/trace` for
the agent SDK's existing `SpanFromGLS` function. Change the `SpanFromContext`
rule to a raw exit snippet that:

1. Keeps the span returned from the supplied context when it is valid.
2. Reads the current GLS span when the context span is invalid.
3. Replaces the result only when the GLS span is non-nil.
4. Recovers any panic raised while inspecting the returned span or reading GLS,
   matching the failure isolation provided by the generic exit trampoline.

This avoids the generic trampoline and preserves the previous nil behavior.
No database statement, span attribute, metric attribute, sampling, exporter,
or redaction behavior changes.

The raw exit snippet installs a nested deferred recovery function before it
examines the returned span. If fallback evaluation panics, the recovery function
leaves the original return value unchanged. If `SpanFromContext` is already
unwinding a panic, the fallback panic is recovered without replacing the
original panic.

## Verification

The existing integration case continues to prove that an HTTP request's active
span can be recovered from GLS when `context.Background()` is supplied.
`testing.AllocsPerRun` additionally requires zero allocations for that
`SpanFromContext` lookup. Regression assertions verify that a valid context span
takes precedence over GLS and that fallback evaluation cannot leak or replace a
panic. The database/sql MySQL integration case verifies that database tracing
remains functional.
