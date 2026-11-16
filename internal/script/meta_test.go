package script

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseMetaDirectives(t *testing.T) {
	src := "-- +migrate Context Test, dev\n-- +migrate Labels billing\n-- +migrate DBMS postgres mysql\n" +
		"-- +migrate RunAlways\n-- +migrate Precondition onFail=mark_ran expect=0 SELECT COUNT(*) FROM T\n" +
		"-- +migrate Up\nSELECT 1;"
	sc, err := Parse([]byte(src), Up)
	if err != nil {
		t.Fatal(err)
	}
	m := sc.Meta
	if !reflect.DeepEqual(m.Contexts, []string{"test", "dev"}) || !reflect.DeepEqual(m.Labels, []string{"billing"}) ||
		!reflect.DeepEqual(m.DBMS, []string{"postgres", "mysql"}) || !m.RunAlways {
		t.Fatalf("meta = %+v", m)
	}
	want := Precondition{SQL: "SELECT COUNT(*) FROM T", Expect: "0", OnFail: MarkRan}
	if len(m.Preconditions) != 1 || m.Preconditions[0] != want {
		t.Fatalf("preconditions = %+v", m.Preconditions)
	}
	for _, bad := range []string{"SELECT 1", "expect=1", "onFail=NOPE expect=1 SELECT 1", "weird=1 expect=1 SELECT 1"} {
		if _, err := Parse([]byte("-- +migrate Precondition "+bad+"\nSELECT 1;"), Up); !errors.Is(err, ErrBadPrecondition) {
			t.Errorf("Precondition %q: err = %v", bad, err)
		}
	}
}
