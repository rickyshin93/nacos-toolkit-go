package nacostoolkit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type sourceFunc func(context.Context, ConfigRef) (string, error)

func (f sourceFunc) GetConfig(ctx context.Context, ref ConfigRef) (string, error) {
	return f(ctx, ref)
}

func TestManagerFailureAndEmptyBatch(t *testing.T) {
	failure := errors.New("injected source error")
	var calls []string
	source := sourceFunc(func(_ context.Context, ref ConfigRef) (string, error) {
		calls = append(calls, ref.DataID)
		if ref.DataID == "failure" {
			return "", failure
		}
		return "env: ${DEPLOY_ENV}\nother: fallback", nil
	})
	ctx := context.Background()
	ref := func(id string) ConfigRef { return ConfigRef{DataID: id, Group: "test"} }
	override := ref("failure")
	manager := NewManager(source, "dev")
	if _, err := manager.Get(ctx, []ConfigRef{ref("first"), ref("failure"), ref("unreachable")}, &override, true); !errors.Is(err, failure) {
		t.Fatalf("base failure = %v", err)
	}
	if raw := manager.Raw(); raw != nil {
		t.Fatalf("partially fetched base batch set raw = %v", raw)
	}
	assertValue(t, calls, []string{"first", "failure"})

	if _, err := manager.Get(ctx, []ConfigRef{ref("app")}, &override, true); !errors.Is(err, failure) {
		t.Fatalf("override failure = %v", err)
	}
	assertValue(t, manager.Raw(), wantJSON(t, `{"env":"${DEPLOY_ENV}","other":"fallback"}`))
	assertValue(t, calls, []string{"first", "failure", "app", "failure"})

	emptyOverride := ref("")
	result, err := manager.Get(ctx, nil, &emptyOverride, true)
	if err != nil {
		t.Fatal(err)
	}
	assertValue(t, result.Raw, map[string]any{})
	assertValue(t, result.Config, map[string]any{})
	assertValue(t, calls, []string{"first", "failure", "app", "failure"})
}

func TestManagerConcurrentColdCache(t *testing.T) {
	var calls atomic.Int32
	manager := NewManager(sourceFunc(func(_ context.Context, _ ConfigRef) (string, error) {
		calls.Add(1)
		return "nested: {enabled: true}", nil
	}), "")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			result, err := manager.Get(context.Background(), []ConfigRef{{DataID: "app.yml"}}, nil, true)
			if err != nil {
				t.Error(err)
				return
			}
			item := result.Config.(map[string]any)["nested"].(map[string]any)
			if item["enabled"] != true {
				t.Errorf("cache returned a mutated value: %v", item)
			}
			item["enabled"] = false
			result.Raw["nested"].(map[string]any)["enabled"] = false
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("cold fetches = %d", calls.Load())
	}
}
