package didit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/didit-protocol/sdk-go/internal/apijson"
)

// Struct tags of generated parameter types:
//
//	json:"name"    a JSON body field (also its multipart field name)
//	form:"name"    a multipart-only field, such as a file
//	query:"name"   a query parameter
//	header:"Name"  a header
//	body:"json"    the whole JSON body (an operation whose body is a schema
//	               component and that also has query or header parameters)

var fileType = reflect.TypeOf(File{})

// encodeQuery adds the query-tagged fields of params to q.
func encodeQuery(params any, q url.Values) {
	rv, ok := structValue(params)
	if !ok {
		return
	}
	indexes, names := apijson.Fields(rv.Type(), "query")
	for i, idx := range indexes {
		v, ok := scalarValue(rv.Field(idx))
		if !ok {
			continue
		}
		if items, isList := v.([]string); isList {
			for _, item := range items {
				q.Add(names[i], item)
			}
			continue
		}
		q.Set(names[i], v.(string))
	}
}

// encodeHeaders sets the header-tagged fields of params on h.
func encodeHeaders(params any, h http.Header) {
	rv, ok := structValue(params)
	if !ok {
		return
	}
	indexes, names := apijson.Fields(rv.Type(), "header")
	for i, idx := range indexes {
		v, ok := scalarValue(rv.Field(idx))
		if !ok {
			continue
		}
		if s, isString := v.(string); isString && s != "" {
			h.Set(names[i], s)
		}
	}
}

// encodeJSONBody returns the JSON body of params.
func encodeJSONBody(params any) ([]byte, error) {
	if rv, ok := structValue(params); ok {
		indexes, _ := apijson.Fields(rv.Type(), "body")
		if len(indexes) == 1 {
			return marshalJSON(rv.Field(indexes[0]).Interface())
		}
	}
	return marshalJSON(params)
}

// hasFile reports whether any file field of params is set.
func hasFile(params any) bool {
	rv, ok := structValue(params)
	if !ok {
		return false
	}
	indexes, _ := apijson.Fields(rv.Type(), "form")
	for _, idx := range indexes {
		if f, ok := fileValue(rv.Field(idx)); ok && !f.IsZero() {
			return true
		}
	}
	return false
}

// encodeMultipart returns the multipart/form-data body of params and its
// content type. Files are read here, once, so that retries resend the same
// bytes.
func encodeMultipart(params any) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	rv, ok := structValue(params)
	if ok {
		for _, tag := range []string{"json", "form"} {
			indexes, names := apijson.Fields(rv.Type(), tag)
			for i, idx := range indexes {
				if err := writeFormField(w, names[i], rv.Field(idx)); err != nil {
					return nil, "", err
				}
			}
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

func writeFormField(w *multipart.Writer, name string, v reflect.Value) error {
	if f, ok := fileValue(v); ok {
		if f.IsZero() {
			return nil
		}
		data, contentType, err := f.load()
		if err != nil {
			return err
		}
		filename := f.name
		if filename == "" {
			filename = name
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(name), escapeQuotes(filename)))
		h.Set("Content-Type", contentType)
		part, err := w.CreatePart(h)
		if err != nil {
			return err
		}
		_, err = part.Write(data)
		return err
	}
	// Unwrap Opt.
	if o, ok := v.Interface().(optional); ok {
		value, set := o.anyValue()
		if !set {
			return nil
		}
		v = reflect.ValueOf(value)
	}
	if !v.IsValid() {
		return nil
	}
	if s, ok := formScalar(v); ok {
		return w.WriteField(name, s)
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 {
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			if s, ok := formScalar(item); ok {
				if err := w.WriteField(name, s); err != nil {
					return err
				}
				continue
			}
			raw, err := marshalJSON(item.Interface())
			if err != nil {
				return err
			}
			if err := w.WriteField(name, string(raw)); err != nil {
				return err
			}
		}
		return nil
	}
	if (v.Kind() == reflect.Map || v.Kind() == reflect.Interface) && v.IsNil() {
		return nil
	}
	// Objects (metadata, ...) are sent as a JSON string.
	raw, err := marshalJSON(v.Interface())
	if err != nil {
		return err
	}
	return w.WriteField(name, string(raw))
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "", "\n", "")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }

func fileValue(v reflect.Value) (File, bool) {
	if v.Type() == fileType {
		return v.Interface().(File), true
	}
	if o, ok := v.Interface().(optional); ok {
		value, set := o.anyValue()
		if f, isFile := value.(File); isFile {
			if !set {
				return File{}, true
			}
			return f, true
		}
	}
	return File{}, false
}

// formScalar formats strings, numbers, booleans and times.
func formScalar(v reflect.Value) (string, bool) {
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if t, ok := v.Interface().(time.Time); ok {
		return t.Format(time.RFC3339Nano), true
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), true
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), true
	}
	return "", false
}

// scalarValue returns a query or header value: a string, a []string, or
// false when the field is omitted or null.
func scalarValue(v reflect.Value) (any, bool) {
	if o, ok := v.Interface().(optional); ok {
		value, set := o.anyValue()
		if !set {
			return nil, false
		}
		v = reflect.ValueOf(value)
	}
	if s, ok := formScalar(v); ok {
		return s, true
	}
	if v.Kind() == reflect.Slice {
		items := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			if s, ok := formScalar(v.Index(i)); ok {
				items = append(items, s)
			}
		}
		return items, true
	}
	return nil, false
}

func structValue(params any) (reflect.Value, bool) {
	if params == nil {
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(params)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, false
		}
		rv = rv.Elem()
	}
	return rv, rv.Kind() == reflect.Struct
}

// rawJSON is used for schema-less JSON responses.
type rawJSON = json.RawMessage
