package apijson

import (
	"encoding/json"
	"testing"
	"time"
)

type opt struct {
	set bool
	v   string
}

func (o opt) IsOmitted() bool { return !o.set }
func (o opt) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.v)
}

type params struct {
	Name    string `json:"name"`
	Nick    opt    `json:"nick"`
	Skip    string `json:"-"`
	private string
	HTML    string `json:"html,omitempty"`
}

func TestMarshalObject(t *testing.T) {
	got, err := MarshalObject(params{Name: "a", HTML: "<&>", private: "x"})
	if err != nil || string(got) != `{"name":"a","html":"<&>"}` {
		t.Fatalf("got %s, %v", got, err)
	}
	got, _ = MarshalObject(&params{Nick: opt{set: true, v: "n"}})
	if string(got) != `{"name":"","nick":"n","html":""}` {
		t.Fatalf("got %s", got)
	}
	var nilParams *params
	if got, _ := MarshalObject(nilParams); string(got) != "null" {
		t.Fatalf("nil: %s", got)
	}
	if _, err := MarshalObject(struct {
		C chan int `json:"c"`
	}{}); err == nil {
		t.Fatal("want an error for an unsupported type")
	}
}

type inner struct {
	A int64 `json:"a"`
}

type resp struct {
	S     string            `json:"s"`
	I     int64             `json:"i"`
	F     float64           `json:"f"`
	B     bool              `json:"b"`
	T     time.Time         `json:"t"`
	L     []int64           `json:"l"`
	M     map[string]int64  `json:"m"`
	O     inner             `json:"o"`
	Any   any               `json:"any"`
	Bad   int64             `json:"bad"`
	BadL  []int64           `json:"bad_l"`
	BadM  map[string]int64  `json:"bad_m"`
	BadT  time.Time         `json:"bad_t"`
	BadTS time.Time         `json:"bad_ts"`
	BadB  bool              `json:"bad_b"`
	BadF  float64           `json:"bad_f"`
	BadS  string            `json:"bad_s"`
	Nulls map[string]string `json:"nulls"`
}

func TestUnmarshalObjectCoerces(t *testing.T) {
	data := `{
		"s": 12.5, "i": "42", "f": "1.5", "b": "true", "t": "2025-06-01 10:00:00",
		"l": [1, "2", null, 3.0], "m": {"x": "7", "y": null}, "o": {"a": 1},
		"any": [1, "a"], "bad": "x", "bad_l": "x", "bad_m": [1], "bad_t": 5,
		"bad_ts": "yesterday", "bad_b": 3, "bad_f": true, "bad_s": {"a": 1},
		"nulls": null, "extra": 1
	}`
	var r resp
	meta, err := UnmarshalObject([]byte(data), &r)
	if err != nil {
		t.Fatal(err)
	}
	if r.S != "12.5" || r.I != 42 || r.F != 1.5 || !r.B || r.T.Hour() != 10 || r.O.A != 1 {
		t.Errorf("coerced %+v", r)
	}
	if len(r.L) != 4 || r.L[1] != 2 || r.L[2] != 0 || r.L[3] != 3 {
		t.Errorf("list %v", r.L)
	}
	if r.M["x"] != 7 || r.M["y"] != 0 {
		t.Errorf("map %v", r.M)
	}
	for _, k := range []string{"bad", "bad_l", "bad_m", "bad_t", "bad_ts", "bad_b", "bad_f", "bad_s"} {
		if meta.Fields[k] != FieldInvalid {
			t.Errorf("%s: state %d, want invalid", k, meta.Fields[k])
		}
	}
	if meta.Fields["nulls"] != FieldNull || meta.Fields["s"] != FieldPresent {
		t.Error("states")
	}
	if _, ok := meta.Extra["extra"]; !ok || len(meta.Extra) != 1 {
		t.Errorf("extra %v", meta.Extra)
	}
}

func TestUnmarshalObjectNonObjects(t *testing.T) {
	var r resp
	for _, data := range []string{`null`, `[1,2]`, `"text"`, ``, `  `} {
		meta, err := UnmarshalObject([]byte(data), &r)
		if err != nil || meta.Fields != nil || string(meta.Raw) != data {
			t.Errorf("%q: %+v %v", data, meta, err)
		}
	}
	for _, data := range []string{`{bad`, `[1,`} {
		if _, err := UnmarshalObject([]byte(data), &r); err == nil {
			t.Errorf("%q: want an error", data)
		}
	}
}

func TestIntOverflowIsInvalid(t *testing.T) {
	var r struct {
		N int8 `json:"n"`
	}
	meta, _ := UnmarshalObject([]byte(`{"n": "300"}`), &r)
	if meta.Fields["n"] != FieldInvalid {
		t.Fatal("overflow accepted")
	}
}

func TestFields(t *testing.T) {
	idx, names := Fields(reflectTypeOf(params{}), "json")
	if len(idx) != 3 || names[0] != "name" || names[2] != "html" {
		t.Fatalf("%v %v", idx, names)
	}
}
