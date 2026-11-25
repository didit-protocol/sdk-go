package webhook

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"
)

// TestPyRepr checks appendPyRepr against Python's repr(float), taken from
// CPython 3.11. X-Signature-Simple uses repr for floats, so integral values
// keep their ".0" here.
func TestPyRepr(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{0.1, "0.1"},
		{1e-05, "1e-05"},
		{0.0001, "0.0001"},
		{1e16, "1e+16"},
		{1e15, "1000000000000000.0"},
		{9999999999999998.0, "9999999999999998.0"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		{5e-324, "5e-324"},
		{123456789.123456789, "123456789.12345679"},
		{1e22, "1e+22"},
		{0.30000000000000004, "0.30000000000000004"},
		{-2.5e-07, "-2.5e-07"},
		{1.5e-07, "1.5e-07"},
		{1.2345e+20, "1.2345e+20"},
		{0.5, "0.5"},
		{math.Copysign(0, -1), "-0.0"},
		{0, "0.0"},
		{87, "87.0"},
		{100, "100.0"},
		{1.5e300, "1.5e+300"},
		{2.2250738585072014e-308, "2.2250738585072014e-308"},
		{1.0 / 3, "0.3333333333333333"},
		{1.0000000000000002, "1.0000000000000002"},
		{4.35, "4.35"},
		{1234567890123456.7, "1234567890123456.8"},
		{12345678901234567.0, "1.2345678901234568e+16"},
		{9.999999999999999e-05, "9.999999999999999e-05"},
		{299792458, "299792458.0"},
		{-1e-05, "-1e-05"},
	}
	for _, c := range cases {
		if got := string(appendPyRepr(nil, c.f)); got != c.want {
			t.Errorf("repr(%s) = %s, want %s", strconv.FormatFloat(c.f, 'g', -1, 64), got, c.want)
		}
	}
}

func TestAppendIntegral(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{-(1 << 63), "-9223372036854775808"},
		{1 << 63, "9223372036854775808"},
		{1e16, "10000000000000000"},
		{-1e19, "-10000000000000000000"},
	}
	for _, c := range cases {
		if got := string(appendIntegral(nil, c.f)); got != c.want {
			t.Errorf("appendIntegral(%g) = %s, want %s", c.f, got, c.want)
		}
	}
}

func TestIsJSONNumber(t *testing.T) {
	valid := []string{"0", "-0", "1", "-1", "10", "1.5", "0.5", "1e5", "1E+5", "1e-5", "-0.0e0", "123.456e-789"}
	invalid := []string{"", "-", "+1", "01", "1.", ".5", "1e", "1e+", "1ex", "0x10", "1.5.5", "--1", "1 ", "NaN", "Infinity", "1_000"}
	for _, s := range valid {
		if !isJSONNumber(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range invalid {
		if isJSONNumber(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestMeasureAndLimit(t *testing.T) {
	v := map[string]any{"a": []any{json.Number("1e300"), 1e22, -1e19, json.Number("12"), 2.5, "x\u0001"}, "b": nil}
	full, err := canonicalJSON(v, 0)
	if err != nil {
		t.Fatal(err)
	}
	// measureJSON counts the floats beyond the int64 range in short form:
	// 1e+300 for 301 digits, 1e+22 for 23, -1e+19 for 21.
	n, err := measureJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := len(full) - (301 - 6) - (23 - 5) - (21 - 6); n != want {
		t.Errorf("measureJSON = %d, want %d (canonical JSON %s)", n, want, full)
	}
	small := map[string]any{"a": []any{json.Number("12"), 2.5, 1e18, "x\u0001"}, "b": nil}
	full, _ = canonicalJSON(small, 0)
	if n, _ := measureJSON(small); n != len(full) {
		t.Errorf("measureJSON = %d, want the canonical length %d", n, len(full))
	}

	// The limit is inclusive, and checked before each value and at the end.
	if got, err := canonicalJSON(small, len(full)); err != nil || string(got) != string(full) {
		t.Errorf("at the limit: %s, %v", got, err)
	}
	for _, limit := range []int{1, len(full) - 1} {
		if _, err := canonicalJSON(small, limit); !errors.Is(err, errTooLong) {
			t.Errorf("limit %d: err = %v, want errTooLong", limit, err)
		}
	}
	if _, err := measureJSON(map[string]any{"a": math.NaN()}); err == nil {
		t.Error("measureJSON accepted NaN")
	}
}

func TestDecimalText(t *testing.T) {
	for in, want := range map[string]string{
		"0": "0", "-0": "0", "+0": "0", "000": "0", "-000": "0", "7": "7", "+007": "7", "-007": "-7", "1760702400": "1760702400",
	} {
		if got, ok := decimalText(in); !ok || got != want {
			t.Errorf("decimalText(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "-", "+", "+-1", "--1", "1.0", "1e3", " 1", "0x10", "1_000"} {
		if got, ok := decimalText(in); ok {
			t.Errorf("decimalText(%q) = %q, want failure", in, got)
		}
	}
}
