package didit

import (
	"encoding/json"

	"github.com/didit-protocol/sdk-go/internal/apijson"
)

// ResponseJSON describes the JSON object a response type was decoded from.
// Every generated response type has one in its JSON field:
//
//	decision.JSON.Raw()                // the object as received
//	decision.JSON.Present("metadata")  // the key was sent (maybe as null)
//	decision.JSON.Null("metadata")     // the key was sent as null
//	decision.JSON.ExtraFields()        // keys this SDK version does not know
//
// Decoding is lenient: a value that does not fit its Go field leaves the field
// at its zero value and Invalid reports it; the value stays readable through
// Field.
type ResponseJSON struct {
	meta apijson.Metadata
}

// Raw returns the JSON object as received.
func (j ResponseJSON) Raw() string { return string(j.meta.Raw) }

// Present reports whether the object had the key, even with a null value.
// It covers known and unknown keys.
func (j ResponseJSON) Present(key string) bool {
	if _, ok := j.meta.Fields[key]; ok {
		return true
	}
	_, ok := j.meta.Extra[key]
	return ok
}

// Null reports whether the object had the key with a null value.
func (j ResponseJSON) Null(key string) bool {
	if state, ok := j.meta.Fields[key]; ok {
		return state == apijson.FieldNull
	}
	raw, ok := j.meta.Extra[key]
	return ok && string(raw) == "null"
}

// Invalid reports whether the key's value did not fit the Go field, which then
// holds its zero value.
func (j ResponseJSON) Invalid(key string) bool {
	return j.meta.Fields[key] == apijson.FieldInvalid
}

// Field returns the raw JSON value of any key, known or unknown.
func (j ResponseJSON) Field(key string) (json.RawMessage, bool) {
	if raw, ok := j.meta.Extra[key]; ok {
		return raw, true
	}
	if _, ok := j.meta.Fields[key]; !ok {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(j.meta.Raw, &obj); err != nil {
		return nil, false
	}
	raw, ok := obj[key]
	return raw, ok
}

// ExtraFields returns the keys of the object that this SDK version does not
// know, with their raw values. Newer API fields appear here until the SDK is
// updated.
func (j ResponseJSON) ExtraFields() map[string]json.RawMessage {
	return j.meta.Extra
}

// unmarshalResponse decodes data into dst, a pointer to a generated response
// struct, and records the metadata in meta.
func unmarshalResponse(data []byte, dst any, meta *ResponseJSON) error {
	m, err := apijson.UnmarshalObject(data, dst)
	meta.meta = m
	return err
}

// marshalJSON encodes v without HTML escaping.
func marshalJSON(v any) ([]byte, error) { return apijson.Marshal(v) }

// marshalParams encodes a generated parameter struct, skipping omitted fields.
func marshalParams(v any) ([]byte, error) { return apijson.MarshalObject(v) }
