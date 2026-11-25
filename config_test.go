package nacostoolkit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func assertValue(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		encode := func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		}
		t.Fatalf("got %s (%#v), want %s (%#v)", encode(got), got, encode(want), want)
	}
}

func wantJSON(t *testing.T, text string) any {
	t.Helper()
	result, err := decode([]byte(text), JSON)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCrosscheckRustPython(t *testing.T) {
	data, err := os.ReadFile("testdata/crosscheck.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want, err := ParseConfigFile("testdata/crosscheck.json")
	if err != nil {
		t.Fatal(err)
	}
	assertValue(t, Process(string(data), YAML, map[string]any{"EXTRA": "x"}, nil), want)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		format Format
		want   string
	}{
		{"nested YAML", "root: {enabled: true, hosts: [a, b], port: 8848}", YAML, `{"root":{"enabled":true,"hosts":["a","b"],"port":8848}}`},
		{"plain scalars", "date: 2026-01-02\nleading: 0777\nmodern: 0o777\nunderscore: 1_000\nyes: yes\non: on\nnonfinite: .nan\nfloat: 1e3\nhex: -0x10", YAML, `{"date":"2026-01-02","leading":"0777","modern":511,"underscore":"1_000","yes":"yes","on":"on","nonfinite":null,"float":1000.0,"hex":-16}`},
		{"YAML keys", "true: true\n1: number\nnull: null\n01: leading\n1: last", YAML, `{"true":true,"1":"last","null":null,"01":"leading"}`},
		{"YAML aliases", "base: &base {a: 1}\na: {<<: *base, b: 2}", YAML, `{"a":{"<<":{"a":1},"b":2},"base":{"a":1}}`},
		{"unsupported YAML tag", "a: !custom {key: value}", YAML, "{}"},
		{"explicit tags", "float: !!float 010\nnonfinite: !!float .inf\nbinary: !!binary Zg==", YAML, `{"float":10.0,"nonfinite":null,"binary":"Zg=="}`},
		{"invalid explicit tag", "float: !!float 0x10", YAML, "{}"},
		{"overflow YAML", "a: 18446744073709551616", YAML, "{}"},
		{"recursive alias", "node: &node [*node]", YAML, "{}"},
		{"multiple YAML documents", "a: 1\n---\nb: 2", YAML, "{}"},
		{"invalid YAML", "root: [", YAML, "{}"},
		{"YAML list", "[1, 2]", YAML, "{}"},
		{"YAML scalar", "123", YAML, "{}"},
		{"YAML keys cannot be collections", "? [a, b]\n: 1", YAML, "{}"},
		{"empty YAML", "", YAML, "{}"},
		{"JSON object", `{"n":18446744073709551615,"v":null}`, JSON, `{"n":18446744073709551615,"v":null}`},
		{"JSON list", `[1, true, "app"]`, JSON, `[1,true,"app"]`},
		{"JSON scalar", `"app"`, JSON, `"app"`},
		{"JSON null", "null", JSON, "null"},
		{"invalid JSON", "{oops}", JSON, "{}"},
		{"multiple JSON values", "{} {}", JSON, "{}"},
		{"overflow JSON", "1e400", JSON, "{}"},
		{"YAML and JSON float representation", "a: 1e20\nb: 1e15\nc: 1.0\nd: 1e-6\ne: 1e-5", YAML, `{"a":1e+20,"b":1000000000000000.0,"c":1.0,"d":1e-6,"e":0.00001}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertValue(t, Parse(tt.input, tt.format), wantJSON(t, tt.want))
		})
	}
	assertValue(t, Parse(map[string]any{"x": true}, JSON), map[string]any{"x": true})
	assertValue(t, Parse([]any{1}, YAML), map[string]any{})
}

func TestLocalConfig(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	check := func(want string, value any) {
		t.Helper()
		if path, ok := FindLocalConfig("app", dir); !ok || path != want {
			t.Fatalf("FindLocalConfig = %q, %t, want %q", path, ok, want)
		}
		got, ok := GetLocalConfig("app", dir)
		if !ok {
			t.Fatal("GetLocalConfig did not return the config")
		}
		assertValue(t, got, value)
	}
	check(write("app.yml", "src: yml"), wantJSON(t, `{"src":"yml"}`))
	check(write("app.yaml", "src: yaml"), wantJSON(t, `{"src":"yaml"}`))
	check(write("app.json", `{"src":"json"}`), wantJSON(t, `{"src":"json"}`))

	for _, name := range []string{"other", ""} {
		if value, ok := GetLocalConfig(name, dir); value != nil || ok {
			t.Fatalf("GetLocalConfig(%q) = %v, %t", name, value, ok)
		}
	}
	for _, tt := range []struct{ name, data, want string }{
		{"top.YML", "[1, 2]", "[1,2]"},
		{"empty.yaml", "", "null"},
		{"comment.yaml", "# empty document\n", "null"},
		{"null.json", "null", "null"},
	} {
		got, err := ParseConfigFile(write(tt.name, tt.data))
		if err != nil {
			t.Fatal(err)
		}
		assertValue(t, got, wantJSON(t, tt.want))
	}
	if got, ok := GetLocalConfig("null", dir); got != nil || !ok {
		t.Fatalf("valid null config = %#v, %t", got, ok)
	}
	if _, err := ParseConfigFile(write("app.ini", "x=y")); err == nil || !strings.Contains(err.Error(), "unsupported file format") {
		t.Fatalf("unsupported extension: %v", err)
	}
	if _, err := ParseConfigFile(write(".json", "{}")); err == nil || !strings.Contains(err.Error(), "unsupported file format") {
		t.Fatalf("dotfile extension: %v", err)
	}
	if _, found := FindLocalConfig("app", ""); found {
		t.Fatal("empty config directory matched a file")
	}
	if _, err := ParseConfigFile(filepath.Join(dir, "missing.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := ParseConfigFile(write("bad.yml", "x: [")); err == nil {
		t.Fatal("invalid YAML file returned no error")
	}
	if _, err := ParseConfigFile(write("badutf8.json", "\"\xff\"")); err == nil {
		t.Fatal("invalid UTF-8 file returned no error")
	}
	write("app.json", "bad JSON")
	if _, ok := GetLocalConfig("app", dir); ok {
		t.Fatal("higher-priority invalid file must not fall back to YAML")
	}
}

func TestDeepMerge(t *testing.T) {
	base := wantJSON(t, `{"n":{"keep":1,"replace":[1,2],"scalar":"prev"},"x":true}`)
	override := wantJSON(t, `{"n":{"replace":[3],"scalar":{"new":1}},"x":null,"new":[true]}`)
	result := Merge(base, override).(map[string]any)
	assertValue(t, result, wantJSON(t, `{"n":{"keep":1,"replace":[3],"scalar":{"new":1}},"x":null,"new":[true]}`))
	result["n"].(map[string]any)["keep"] = false
	result["new"].([]any)[0] = false
	assertValue(t, base, wantJSON(t, `{"n":{"keep":1,"replace":[1,2],"scalar":"prev"},"x":true}`))
	assertValue(t, override, wantJSON(t, `{"n":{"replace":[3],"scalar":{"new":1}},"x":null,"new":[true]}`))
	assertValue(t, Merge(base, nil), nil)
}

func TestTemplateTypesAndPrecedence(t *testing.T) {
	raw := `
port: ${platform.port}
enabled: ${platform.enabled}
account: ${platform.account}
ids: ${platform.ids}
alias: ${A}
nil: ${NULL}
undefined: ${UNKNOWN}
mixed: id-${platform.port}-${platform.enabled}
entries:
  - ${platform.port}
  - item: ${platform.enabled}
  - ["${platform.port}"]
cors: {whitelist: "a,, b "}
EXTRA: local
winner: ${EXTRA}
`
	vars := wantJSON(t, `{"platform":{"port":8848,"enabled":true,"account":{"project":"${EXTRA}"},"ids":[1,"${EXTRA}"]},"EXTRA":"external","A":"${platform.port}","NULL":null}`).(map[string]any)
	want := `{"port":8848,"enabled":true,"account":{"project":"local"},"ids":[1,"local"],"alias":"8848","nil":"${NULL}","undefined":"${UNKNOWN}","mixed":"id-8848-true","entries":["8848",{"item":true},["${platform.port}"]],"cors":{"whitelist":["a","","b"]},"EXTRA":"local","winner":"local"}`
	assertValue(t, Process(raw, YAML, vars, nil), wantJSON(t, want))
	assertValue(t, Process(`{"cors":{"whitelist":"a, b"}}`, JSON, nil, []string{}), wantJSON(t, `{"cors":{"whitelist":"a, b"}}`))
	assertValue(t, Process("tags: 'a , b'\nkeep: 'a'", YAML, nil, []string{"tags", "keep"}), wantJSON(t, `{"tags":["a","b"],"keep":"a"}`))
	assertValue(t, ProcessAndMerge(
		wantJSON(t, `{"host":"base","db":{"port":10},"cors":{"whitelist":["a"]}}`),
		`{"url":"http://${host}/${DEPLOY_ENV}","db":{"schema":"public"},"cors":{"whitelist":["b"]}}`,
		JSON, map[string]any{"host": "external", "DEPLOY_ENV": "dev1"}, nil,
	), wantJSON(t, `{"host":"base","db":{"port":10,"schema":"public"},"cors":{"whitelist":["b"]},"url":"http://base/dev1"}`))
}

func TestTemplateCyclesAndPassLimit(t *testing.T) {
	vars := map[string]any{"A": "${B}", "B": "${A}", "SELF": "${SELF}+${SELF}", "OBJ": map[string]any{"inner": "${OBJ}"}}
	if !ContainsTemplate("prefix ${A}") || ContainsTemplate("${}") {
		t.Fatal("placeholder detection differs from Rust")
	}
	assertValue(t, RenderText("${A}", vars), "${B}")
	assertValue(t, Render("${A}", vars), "${A}")
	if s := Render("${SELF}", vars).(string); !strings.Contains(s, "${SELF}") {
		t.Fatalf("self cycle should retain a placeholder: %q", s)
	}
	got := Render("${OBJ}", vars)
	for range 6 {
		got = got.(map[string]any)["inner"]
	}
	assertValue(t, got, "${OBJ}")

	vars = map[string]any{"A": "${B}", "B": "${C}", "C": "${D}", "D": "${E}", "E": "${F}", "F": "${G}", "G": "resolved"}
	assertValue(t, RenderText("${A}", vars), "${F}")
	assertValue(t, Render("${A}", vars), "resolved")
	vars["object"] = map[string]any{"url": "${A}"}
	vars["text"] = "object=${object}"
	assertValue(t, Render("${text}", vars), `object={"url":"resolved"}`)
	assertValue(t, Process("n: 1e15\ntiny: 1e-6\ntext: ${n}/${tiny}", YAML, nil, nil).(map[string]any)["text"], "1000000000000000.0/1e-6")
}
