package jsonedit

import (
	"encoding/json"
	"testing"
)

func TestParse_StringsWithBracketsAndEscapes(t *testing.T) {
	doc := []byte(`{"a": "x}]\"{[", "b": [1, {"c": "\\"}], "d": null, "e": -1.5e3}`)
	root, err := Root(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": `"x}]\"{["`, "b": `[1, {"c": "\\"}]`, "d": "null", "e": "-1.5e3"}
	if len(root.Items) != len(want) {
		t.Fatalf("items = %d", len(root.Items))
	}
	for _, it := range root.Items {
		if got := string(it.Value(doc)); got != want[it.Key] {
			t.Errorf("%s = %s, want %s", it.Key, got, want[it.Key])
		}
	}
}

func TestRebuild_DropAndAppendKeepBytes(t *testing.T) {
	doc := []byte("{\n    \"x\": [1,2],\n    \"y\": \"a && b\",\n    \"z\": {}\n}\n")
	root, _ := Root(doc)
	dropped := Rebuild(doc, root, []Item{root.Items[0], root.Items[2]})
	if string(dropped) != "{\n    \"x\": [1,2],\n    \"z\": {}\n}\n" {
		t.Fatalf("drop:\n%s", dropped)
	}
	root, _ = Root(dropped)
	m, err := Member("n", map[string]int{"k": 1}, root.ItemIndent(dropped), IndentUnit(dropped))
	if err != nil {
		t.Fatal(err)
	}
	added := Append(dropped, root, m)
	want := "{\n    \"x\": [1,2],\n    \"z\": {},\n    \"n\": {\n        \"k\": 1\n    }\n}\n"
	if string(added) != want {
		t.Fatalf("append:\n%s\nwant:\n%s", added, want)
	}
	if !json.Valid(added) {
		t.Fatal("invalid result")
	}
}

func TestRebuild_EmptyContainers(t *testing.T) {
	doc := []byte(`{"a":[]}`)
	root, _ := Root(doc)
	arr, _ := Parse(doc, root.Items[0].ValStart)
	if got := string(Append(doc, arr, "1")); got != `{"a":[1]}` {
		t.Fatalf("compact append = %s", got)
	}
	if got := string(Rebuild(doc, root, nil)); got != `{}` {
		t.Fatalf("drop all = %s", got)
	}
	if _, err := Root([]byte(`{"a":`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
