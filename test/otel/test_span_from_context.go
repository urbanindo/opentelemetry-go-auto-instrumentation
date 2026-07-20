// Copyright (c) 2024 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"testing"
	"time"
)

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

func main() {
	go func() {
		http.HandleFunc("/otel", func(writer http.ResponseWriter, request *http.Request) {
			span := trace.SpanFromContext(context.Background())
			if !span.IsRecording() {
				panic("span should be recordedc")
			}
			if !span.SpanContext().IsValid() {
				panic("span should be valid")
			}
			assertFallbackPanicIsolated()
			assertOriginalPanicPreserved()
			assertValidContextSpanPreserved()
			allocs := testing.AllocsPerRun(1000, func() {
				_ = trace.SpanFromContext(context.Background())
			})
			fmt.Printf("SpanFromContext allocs: %.0f\n", allocs)
			fmt.Printf("%v\n", span)
			writer.Write([]byte("hello otel"))
		})
		http.ListenAndServe(":8989", nil)
	}()
	time.Sleep(3 * time.Second)
	client := http.Client{}
	client.Get("http://127.0.0.1:8989/otel")
}
