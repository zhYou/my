package command

import (
	"errors"
	"reflect"
	"testing"
)

func TestTypedArgumentsAreIndependent(t *testing.T) {
	source := []string{"literal", ""}
	spec := Spec[string]{
		Program: "tool",
		Args: []Arg[string]{Literal[string](source...), Value(func(s string) string {
			return s
		})},
	}
	source[0] = "changed"
	a, err := spec.Build("a b")
	if err != nil {
		t.Fatal(err)
	}
	a.Args[0] = "mutated"
	b, err := spec.Build("$HOME")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Args, []string{"literal", "", "$HOME"}) {
		t.Fatal(b)
	}
	if _, err := (Spec[string]{
		Program: "tool",
		Validate: func(string) error {
			return errors.New("invalid")
		},
	}).Build(""); err == nil {
		t.Fatal("缺少校验错误")
	}
}
