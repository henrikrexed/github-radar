package config

import (
	"os"
	"strings"
	"testing"
)

func TestExpandEnvVars_BasicSubstitution(t *testing.T) {
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	input := []byte("token: ${TEST_VAR}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "token: test-value"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_MultipleVariables(t *testing.T) {
	os.Setenv("VAR1", "value1")
	os.Setenv("VAR2", "value2")
	defer os.Unsetenv("VAR1")
	defer os.Unsetenv("VAR2")

	input := []byte("first: ${VAR1}\nsecond: ${VAR2}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "first: value1\nsecond: value2"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_MissingVariable(t *testing.T) {
	os.Unsetenv("MISSING_VAR")

	input := []byte("token: ${MISSING_VAR}")
	_, err := ExpandEnvVars(input)
	if err == nil {
		t.Fatal("ExpandEnvVars() should return error for missing variable")
	}

	expErr, ok := err.(*ExpansionError)
	if !ok {
		t.Fatalf("error should be *ExpansionError, got %T", err)
	}

	if len(expErr.MissingVars) != 1 || expErr.MissingVars[0] != "MISSING_VAR" {
		t.Errorf("MissingVars = %v, want [MISSING_VAR]", expErr.MissingVars)
	}

	if !strings.Contains(err.Error(), "MISSING_VAR") {
		t.Errorf("error message should contain variable name: %v", err)
	}
}

func TestExpandEnvVars_MultipleMissingVariables(t *testing.T) {
	os.Unsetenv("MISSING1")
	os.Unsetenv("MISSING2")

	input := []byte("a: ${MISSING1}\nb: ${MISSING2}")
	_, err := ExpandEnvVars(input)
	if err == nil {
		t.Fatal("ExpandEnvVars() should return error for missing variables")
	}

	expErr, ok := err.(*ExpansionError)
	if !ok {
		t.Fatalf("error should be *ExpansionError, got %T", err)
	}

	if len(expErr.MissingVars) != 2 {
		t.Errorf("MissingVars should have 2 entries, got %d", len(expErr.MissingVars))
	}
}

func TestExpandEnvVars_DefaultValue(t *testing.T) {
	os.Unsetenv("UNSET_VAR")

	input := []byte("value: ${UNSET_VAR:-default-value}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "value: default-value"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_DefaultValueNotUsedWhenSet(t *testing.T) {
	os.Setenv("SET_VAR", "actual-value")
	defer os.Unsetenv("SET_VAR")

	input := []byte("value: ${SET_VAR:-default-value}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "value: actual-value"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_DefaultValueEmpty(t *testing.T) {
	os.Unsetenv("UNSET_VAR")

	// Empty default value
	input := []byte("value: ${UNSET_VAR:-}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "value: "
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_EscapeSequence(t *testing.T) {
	input := []byte("literal: $${NOT_EXPANDED}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "literal: ${NOT_EXPANDED}"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_MixedEscapeAndExpand(t *testing.T) {
	os.Setenv("EXPAND_ME", "expanded")
	defer os.Unsetenv("EXPAND_ME")

	input := []byte("expanded: ${EXPAND_ME}\nliteral: $${KEEP_THIS}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "expanded: expanded\nliteral: ${KEEP_THIS}"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_NoVariables(t *testing.T) {
	input := []byte("plain: value\nother: 123")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	if string(result) != string(input) {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), string(input))
	}
}

func TestExpandEnvVars_VariableInMiddle(t *testing.T) {
	os.Setenv("MIDDLE_VAR", "middle")
	defer os.Unsetenv("MIDDLE_VAR")

	input := []byte("prefix-${MIDDLE_VAR}-suffix")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "prefix-middle-suffix"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_EmptyValue(t *testing.T) {
	os.Setenv("EMPTY_VAR", "")
	defer os.Unsetenv("EMPTY_VAR")

	input := []byte("value: ${EMPTY_VAR}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	// Variable is set but empty - should not error
	expected := "value: "
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpandEnvVars_SpecialCharactersInValue(t *testing.T) {
	os.Setenv("SPECIAL_VAR", "value with spaces & symbols!")
	defer os.Unsetenv("SPECIAL_VAR")

	input := []byte("value: ${SPECIAL_VAR}")
	result, err := ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("ExpandEnvVars() error: %v", err)
	}

	expected := "value: value with spaces & symbols!"
	if string(result) != expected {
		t.Errorf("ExpandEnvVars() = %q, want %q", string(result), expected)
	}
}

func TestExpansionError_Error(t *testing.T) {
	err := &ExpansionError{
		MissingVars: []string{"VAR1", "VAR2"},
	}

	expected := "config expansion failed: missing environment variables: VAR1, VAR2"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
