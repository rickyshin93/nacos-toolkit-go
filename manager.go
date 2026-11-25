package nacostoolkit

import (
	"context"
	"fmt"
	"maps"
	"strings"
)

// Source fetches the raw configuration for a reference.
type Source interface {
	GetConfig(context.Context, ConfigRef) (string, error)
}

// Result contains the rendered config and, in debug mode, the shallow-merged
// raw variable context.
type Result struct {
	Config any
	Raw    map[string]any
}

// Manager loads configuration batches and caches the first successful result.
// Use a separate Manager for a different connection or independent cache.
type Manager struct {
	source    Source
	namespace string

	load   chan struct{}
	cached *Result
	raw    map[string]any
}

// New creates a Manager backed by the Nacos HTTP source.
func New(conn Connection) (*Manager, error) {
	source, err := NewHTTPSource(conn)
	if err != nil {
		return nil, err
	}
	return NewManager(source, conn.Namespace), nil
}

// NewManager creates a Manager with a caller-provided source.
func NewManager(source Source, namespace string) *Manager {
	return &Manager{source: source, namespace: namespace, load: make(chan struct{}, 1)}
}

// Get loads base configurations in order and renders the last one using their
// shallow-merged variables. It then optionally loads and deep-merges override.
// Cache hits ignore base and override until ClearCache.
func (m *Manager) Get(ctx context.Context, base []ConfigRef, override *ConfigRef, debug bool) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	select {
	case m.load <- struct{}{}:
		defer func() { <-m.load }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if m.cached == nil {
		config, err := m.fetch(ctx, base, override)
		if err != nil {
			return Result{}, err
		}
		m.cached = &Result{Config: config, Raw: m.raw}
	}
	result := Result{Config: clone(m.cached.Config)}
	if debug {
		result.Raw = clone(m.cached.Raw).(map[string]any)
	}
	return result, nil
}

func (m *Manager) fetch(ctx context.Context, base []ConfigRef, override *ConfigRef) (any, error) {
	raw := map[string]any{}
	last := map[string]any{}
	for _, ref := range base {
		content, err := m.source.GetConfig(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("fetch base config %q/%q: %w", ref.Group, ref.DataID, err)
		}
		last = Parse(content, YAML).(map[string]any)
		maps.Copy(raw, last)
	}
	m.raw = raw
	vars := maps.Clone(raw)
	vars["DEPLOY_ENV"] = m.namespace
	config := processValue(last, vars, nil)
	if override == nil || override.DataID == "" {
		return config, nil
	}
	content, err := m.source.GetConfig(ctx, *override)
	if err != nil {
		return nil, fmt.Errorf("fetch override config %q/%q: %w", override.Group, override.DataID, err)
	}
	format := YAML
	if strings.HasSuffix(override.DataID, ".json") {
		format = JSON
	}
	return ProcessAndMerge(config, content, format, vars, nil), nil
}

// Raw returns a copy of the last batch's raw base context, or nil before any
// base batch has been fetched. It waits for an active load to finish.
func (m *Manager) Raw() map[string]any {
	m.load <- struct{}{}
	defer func() { <-m.load }()
	if m.raw == nil {
		return nil
	}
	return clone(m.raw).(map[string]any)
}

// ClearCache waits for an active load and clears both config and raw cache.
// The HTTP source retains its login token.
func (m *Manager) ClearCache() {
	m.load <- struct{}{}
	defer func() { <-m.load }()
	m.cached, m.raw = nil, nil
}
