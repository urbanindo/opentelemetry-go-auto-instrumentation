# SpanFromContext Panic Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve the zero-allocation `SpanFromContext` GLS fallback without allowing fallback evaluation to introduce or replace application panics.

**Architecture:** Keep the raw `OnExit` instrumentation and direct `spanFromGLS` linkname. Install a nested deferred recovery function inside the raw exit closure so any panic from `SpanContext` or GLS evaluation is isolated while the normal return value and any pre-existing panic remain unchanged.

**Tech Stack:** Go 1.24+, Loongsuite raw instrumentation rules, OpenTelemetry trace API, integration test harness.

## Global Constraints

- The instrumented steady-state fallback must perform zero allocations.
- A valid span in the supplied context must take precedence over GLS.
- An invalid context span may be replaced only by a non-nil GLS span.
- Fallback evaluation must not introduce a panic or replace an existing panic.
- Database telemetry behavior must remain unchanged.

---

### Task 1: Add panic-isolation regression coverage

**Files:**
- Modify: `test/otel/test_span_from_context.go`

**Interfaces:**
- Consumes: instrumented `trace.SpanFromContext(context.Context) trace.Span`
- Produces: integration assertions for custom-span panic isolation, original-panic preservation, and valid-context precedence

- [ ] **Step 1: Add a typed-nil custom span and a panicking context**

```go
type panickingSpan struct {
	trace.Span
}

func (*panickingSpan) SpanContext() trace.SpanContext {
	panic("span context panic")
}

type panickingContext struct {
	context.Context
	value any
}

func (ctx panickingContext) Value(any) any {
	panic(ctx.value)
}
```

- [ ] **Step 2: Add focused assertions and call them from the active HTTP handler**

```go
func assertFallbackPanicIsolated() {
	defer func() {
		if err := recover(); err != nil {
			panic(fmt.Sprintf("fallback panic escaped: %v", err))
		}
	}()
	ctx := trace.ContextWithSpan(context.Background(), (*panickingSpan)(nil))
	_ = trace.SpanFromContext(ctx)
}

func assertOriginalPanicPreserved() {
	marker := &struct{}{}
	defer func() {
		if err := recover(); err != marker {
			panic(fmt.Sprintf("original panic replaced: %v", err))
		}
	}()
	_ = trace.SpanFromContext(panickingContext{
		Context: context.Background(),
		value:   marker,
	})
}

func assertValidContextSpanPreserved() {
	expected := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{2},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), expected)
	actual := trace.SpanFromContext(ctx).SpanContext()
	if actual.TraceID() != expected.TraceID() ||
		actual.SpanID() != expected.SpanID() {
		panic("valid context span should take precedence over GLS")
	}
}
```

Call all three functions from the HTTP handler before measuring allocations:

```go
assertFallbackPanicIsolated()
assertOriginalPanicPreserved()
assertValidContextSpanPreserved()
```

- [ ] **Step 3: Run the targeted integration test and verify RED**

Run:

```bash
TEST_PLUGIN_NAME=otel-span-from-context-test go test ./test -run '^TestPlugins[1-4]$' -count=1 -v
```

Expected: FAIL because the current raw exit snippet lets the custom
`SpanContext` panic escape.

### Task 2: Restore panic isolation without allocations

**Files:**
- Modify: `tool/data/rules/base.json:140`

**Interfaces:**
- Consumes: named return value `retVal0` and injected `spanFromGLS() Span`
- Produces: raw exit snippet that preserves failure isolation and fallback semantics

- [ ] **Step 1: Add the minimal nested recovery boundary**

Change the raw `OnExit` snippet to install a nested deferred recovery function
before inspecting `retVal0`:

```go
defer func() {
	if err := recover(); err != nil {
		println("failed to exec onExit hook", "SpanFromContext")
	}
}()
if !retVal0.SpanContext().IsValid() {
	if span := spanFromGLS(); span != nil {
		retVal0 = span
	}
}
```

- [ ] **Step 2: Run the targeted integration test and verify GREEN**

Run:

```bash
TEST_PLUGIN_NAME=otel-span-from-context-test go test ./test -run '^TestPlugins[1-4]$' -count=1 -v
```

Expected: PASS with `SpanFromContext allocs: 0`.

- [ ] **Step 3: Run tool tests**

Run:

```bash
go test -count=1 ./tool/...
```

Expected: PASS.

- [ ] **Step 4: Inspect generated instrumentation**

Confirm the generated `go.opentelemetry.io/otel/trace/context.go` contains the
nested recovery defer around the fallback and still calls the direct
`spanFromGLS` linkname.

- [ ] **Step 5: Request focused code review**

Review the PR base through the new head for correctness, panic semantics,
allocation behavior, and test coverage. Fix all Critical and Important findings.

- [ ] **Step 6: Commit and push**

```bash
git add docs/superpowers/specs/2026-07-20-spanfromcontext-allocation-design.md \
  docs/superpowers/plans/2026-07-20-spanfromcontext-panic-isolation.md \
  test/otel/test_span_from_context.go \
  tool/data/rules/base.json
git commit -m "fix(otel-context): isolate SpanFromContext fallback panics"
git push origin fix/spanfromcontext-allocation
```
