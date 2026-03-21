package api

import (
	"errors"
	"testing"
)

func TestApplyJSONPatch_Add(t *testing.T) {
	m := map[string]string{"a": "1"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "add", Path: "b", Value: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["b"] != "2" {
		t.Errorf("expected b=2, got %q", result["b"])
	}
}

func TestApplyJSONPatch_AddExisting(t *testing.T) {
	// Per RFC 6902 §4.1, add to existing key acts as replace
	m := map[string]string{"a": "1"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "add", Path: "a", Value: "99"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["a"] != "99" {
		t.Errorf("expected a=99, got %q", result["a"])
	}
}

func TestApplyJSONPatch_Remove(t *testing.T) {
	m := map[string]string{"a": "1", "b": "2"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "remove", Path: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result["a"]; ok {
		t.Error("expected key a to be removed")
	}
	if result["b"] != "2" {
		t.Errorf("expected b=2, got %q", result["b"])
	}
}

func TestApplyJSONPatch_RemoveNonExistent(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "remove", Path: "z"}})
	if err == nil {
		t.Fatal("expected error removing non-existent key")
	}
}

func TestApplyJSONPatch_Replace(t *testing.T) {
	m := map[string]string{"a": "1"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "replace", Path: "a", Value: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["a"] != "2" {
		t.Errorf("expected a=2, got %q", result["a"])
	}
}

func TestApplyJSONPatch_ReplaceNonExistent(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "replace", Path: "z", Value: "2"}})
	if err == nil {
		t.Fatal("expected error replacing non-existent key")
	}
}

func TestApplyJSONPatch_Test_Pass(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "test", Path: "a", Value: "1"}})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestApplyJSONPatch_Test_Fail(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "test", Path: "a", Value: "99"}})
	if err == nil {
		t.Fatal("expected error on test failure")
	}
	var tfe *testFailedError
	if !errors.As(err, &tfe) {
		t.Errorf("expected testFailedError, got %T: %v", err, err)
	}
}

func TestApplyJSONPatch_Test_Missing(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "test", Path: "z", Value: "1"}})
	if err == nil {
		t.Fatal("expected error on missing key test")
	}
	var tfe *testFailedError
	if !errors.As(err, &tfe) {
		t.Errorf("expected testFailedError, got %T: %v", err, err)
	}
}

func TestApplyJSONPatch_Move_Unsupported(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "move", Path: "b"}})
	if err == nil {
		t.Fatal("expected error for move operation")
	}
}

func TestApplyJSONPatch_Copy_Unsupported(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "copy", Path: "b"}})
	if err == nil {
		t.Fatal("expected error for copy operation")
	}
}

func TestApplyJSONPatch_UnknownOp(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "frobnicate", Path: "a"}})
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestApplyJSONPatch_EmptyOps(t *testing.T) {
	m := map[string]string{"a": "1", "b": "2"}
	result, err := applyJSONPatch(m, []PatchOp{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result["a"] != "1" || result["b"] != "2" {
		t.Errorf("expected original map copy, got %v", result)
	}
}

func TestApplyJSONPatch_PathWithSlash(t *testing.T) {
	m := map[string]string{"FOO": "bar"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "test", Path: "/FOO", Value: "bar"}})
	if err != nil {
		t.Fatalf("expected /FOO to work, got %v", err)
	}
	if result["FOO"] != "bar" {
		t.Errorf("expected FOO=bar, got %q", result["FOO"])
	}
}

func TestApplyJSONPatch_PathWithoutSlash(t *testing.T) {
	m := map[string]string{"FOO": "bar"}
	result, err := applyJSONPatch(m, []PatchOp{{Op: "test", Path: "FOO", Value: "bar"}})
	if err != nil {
		t.Fatalf("expected FOO (no slash) to work, got %v", err)
	}
	if result["FOO"] != "bar" {
		t.Errorf("expected FOO=bar, got %q", result["FOO"])
	}
}

func TestApplyJSONPatch_EmptyPath(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{{Op: "add", Path: "", Value: "x"}})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestApplyJSONPatch_DoesNotMutateInput(t *testing.T) {
	m := map[string]string{"a": "1"}
	_, err := applyJSONPatch(m, []PatchOp{
		{Op: "add", Path: "b", Value: "2"},
		{Op: "remove", Path: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m["a"] != "1" {
		t.Error("original map was mutated: key a changed")
	}
	if _, ok := m["b"]; ok {
		t.Error("original map was mutated: key b was added")
	}
}

func TestApplyJSONPatch_MultipleOps(t *testing.T) {
	m := map[string]string{"a": "1"}
	result, err := applyJSONPatch(m, []PatchOp{
		{Op: "add", Path: "b", Value: "2"},
		{Op: "replace", Path: "a", Value: "10"},
		{Op: "remove", Path: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["a"] != "10" {
		t.Errorf("expected a=10, got %q", result["a"])
	}
	if _, ok := result["b"]; ok {
		t.Error("expected b to be removed")
	}
}

func TestTestFailedError(t *testing.T) {
	_, err := applyJSONPatch(
		map[string]string{"k": "a"},
		[]PatchOp{{Op: "test", Path: "k", Value: "b"}},
	)
	var tfe *testFailedError
	if !errors.As(err, &tfe) {
		t.Fatal("expected testFailedError")
	}
}
