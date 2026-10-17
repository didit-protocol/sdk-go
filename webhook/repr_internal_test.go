package webhook

import (
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
