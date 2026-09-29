package todo

import (
	"reflect"
	"testing"
	"time"

	"github.com/Iliorn/tjek/hlc"
)

// Every Todo field a user can change belongs to a merge unit, or is named
// here with the reason it is not one. A new field fails this test until it is
// one or the other, so the sync merge cannot quietly drop edits to it.
func TestFieldsCoverTheTodo(t *testing.T) {
	notAUnit := map[string]string{
		"ID":           "identity",
		"CreatedAt":    "set once, when the task is made",
		"ModifiedAt":   "a summary of the stamps",
		"Tags":         "a set: one unit per member",
		"Dependencies": "a set: one unit per member",
		"Comments":     "records with their own IDs",
		"TimeEntries":  "records with their own IDs",
		"Stamps":       "the stamps themselves",
		"DeletedAt":    "travels with the deleted unit",
	}
	typ := reflect.TypeOf(Todo{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if _, ok := notAUnit[name]; ok {
			continue
		}
		a, b := Todo{}, Todo{}
		v := reflect.ValueOf(&b).Elem().Field(i)
		switch v.Kind() {
		case reflect.String:
			v.SetString("changed")
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v.SetInt(v.Int() + 1)
		case reflect.Bool:
			v.SetBool(true)
		case reflect.Struct:
			v.Set(reflect.ValueOf(time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)))
		default:
			t.Fatalf("field %s: kind %s is not handled by this test", name, v.Kind())
		}
		var unit *Field
		for j := range Fields {
			if !Fields[j].Same(&a, &b) {
				unit = &Fields[j]
			}
		}
		if unit == nil {
			t.Errorf("field %s is in no unit of Fields", name)
			continue
		}
		unit.Copy(&a, &b)
		if !reflect.DeepEqual(reflect.ValueOf(a).Field(i).Interface(), v.Interface()) {
			t.Errorf("field %s: unit %q compares it but does not copy it", name, unit.Key)
		}
	}
}

// A task saved before stamps existed reads its stamps from the times it
// records; a recorded stamp wins over that; a set member it never held has
// no stamp.
func TestStampFallsBackToTheRecordedTimes(t *testing.T) {
	mod := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	del := mod.Add(time.Hour)
	task := Todo{ModifiedAt: mod, Tags: []string{"home"}}

	if got := task.Stamp("title"); got != hlc.At(mod) {
		t.Errorf("title stamp %q, want the modification time's %q", got, hlc.At(mod))
	}
	if got := task.Stamp(TagKey("home")); got != hlc.At(mod) {
		t.Errorf("present tag stamp %q, want %q", got, hlc.At(mod))
	}
	if got := task.Stamp(TagKey("never")); got != "" {
		t.Errorf("a tag never held has stamp %q", got)
	}
	task.Deleted, task.DeletedAt = true, del
	if got := task.Stamp("deleted"); got != hlc.At(del) {
		t.Errorf("deleted stamp %q, want the deletion's %q", got, hlc.At(del))
	}

	real := hlc.New("aaaa", "").Now(mod)
	task.Stamps = map[string]hlc.Stamp{"title": real, TagKey("gone"): real}
	if task.Stamp("title") != real || task.Stamp(TagKey("gone")) != real {
		t.Error("a recorded stamp should win over the reconstructed one")
	}
	if !task.LatestStamp().After(real) {
		t.Errorf("LatestStamp %q should be the deletion, the latest moment", task.LatestStamp())
	}
}

func TestSetKeysIncludeRemovedMembers(t *testing.T) {
	task := Todo{Tags: []string{"home"}, Dependencies: []string{"d1"},
		Stamps: map[string]hlc.Stamp{TagKey("gone"): "x", "title": "y"}}
	got := map[string]bool{}
	for _, k := range task.SetKeys() {
		got[k] = true
	}
	for _, k := range []string{TagKey("home"), DepKey("d1"), TagKey("gone")} {
		if !got[k] {
			t.Errorf("SetKeys misses %q", k)
		}
	}
	if got["title"] || len(got) != 3 {
		t.Errorf("SetKeys = %v, want only the three set members", got)
	}
	if !task.HasMember(TagKey("home")) || task.HasMember(TagKey("gone")) || task.HasMember("title") {
		t.Error("HasMember reports the members held, and only set keys")
	}
}
