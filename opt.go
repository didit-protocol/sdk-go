package didit

import (
	"encoding/json"
	"fmt"
	"time"
)

type optState uint8

const (
	optOmitted optState = iota
	optValue
	optNull
)

// Opt is an optional request field. Its zero value is omitted: the field is
// not sent at all, so a Go zero value is never sent by accident.
//
//	didit.SessionsCreateParams{
//		WorkflowID: "...",
//		VendorData: didit.String("user-42"),    // sent
//		Callback:   didit.Null[string](),       // sent as null
//		                                         // other Opt fields: not sent
//	}
//
// Use the constructors String, Int, Float, Bool, Time, Some and Null.
type Opt[T any] struct {
	value T
	state optState
}

// Some returns an Opt holding v.
func Some[T any](v T) Opt[T] { return Opt[T]{value: v, state: optValue} }

// Null returns an Opt sent as JSON null.
func Null[T any]() Opt[T] { return Opt[T]{state: optNull} }

// String returns an Opt holding s.
func String(s string) Opt[string] { return Some(s) }

// Int returns an Opt holding n.
func Int(n int64) Opt[int64] { return Some(n) }

// Float returns an Opt holding f.
func Float(f float64) Opt[float64] { return Some(f) }

// Bool returns an Opt holding b.
func Bool(b bool) Opt[bool] { return Some(b) }

// Time returns an Opt holding t.
func Time(t time.Time) Opt[time.Time] { return Some(t) }

// Value returns the value and true when o holds a value, and the zero value
// and false when o is omitted or null.
func (o Opt[T]) Value() (T, bool) { return o.value, o.state == optValue }

// Or returns the value of o, or fallback when o holds no value.
func (o Opt[T]) Or(fallback T) T {
	if o.state == optValue {
		return o.value
	}
	return fallback
}

// IsOmitted reports whether o is not sent.
func (o Opt[T]) IsOmitted() bool { return o.state == optOmitted }

// IsNull reports whether o is sent as JSON null.
func (o Opt[T]) IsNull() bool { return o.state == optNull }

// IsSet reports whether o holds a value (not omitted and not null).
func (o Opt[T]) IsSet() bool { return o.state == optValue }

func (o Opt[T]) anyValue() (any, bool) { return o.value, o.state == optValue }

// MarshalJSON encodes the value, or null. Omitted values are skipped by the
// enclosing parameter type and also encode as null when marshalled directly.
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if o.state != optValue {
		return []byte("null"), nil
	}
	return marshalJSON(o.value)
}

// UnmarshalJSON sets o from JSON: null gives Null, anything else a value.
func (o *Opt[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*o = Null[T]()
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*o = Some(v)
	return nil
}

// String formats the value, "null" or "omitted".
func (o Opt[T]) String() string {
	switch o.state {
	case optValue:
		return fmt.Sprint(o.value)
	case optNull:
		return "null"
	default:
		return "omitted"
	}
}

// optional is implemented by every Opt.
type optional interface {
	IsOmitted() bool
	IsNull() bool
	anyValue() (any, bool)
}
