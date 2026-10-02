// Package canon is one encoding per value, written down rather than borrowed.
//
// Several things in Rokh are named by the hash of their JSON bytes, or carry
// JSON inside bytes that get signed. For those, "the same content has exactly
// one correct encoding" is the law, and a second encoding — even with one extra
// space — is refused rather than accepted and repaired.
//
// encoding/json cannot serve that law, and the reason is not a detail:
//
//   - It escapes the three characters < > & as \u003c, \u003e and \u0026.
//     No other language does this by default, so the same value encoded by a
//     Go implementation and a Python one gives different bytes, a different
//     name, and — where the law is enforced — an interop failure that looks
//     like corruption.
//   - It escapes U+2028 and U+2029 even with SetEscapeHTML(false).
//   - It replaces invalid UTF-8 with U+FFFD, silently, which gives two
//     different inputs one name.
//   - It writes a byte array as a list of numbers, so a 32-byte id becomes
//     [62,35,232,...] rather than its hex.
//
// So the byte order is stated here:
//
//   - Object keys are sorted by their UTF-16 code units, ascending, and every
//     key appears once. Declaration order is a Go fact and not a shared one.
//   - Strings escape a quote and a backslash; backspace, tab, newline, form
//     feed and carriage return take their short escapes; other code points
//     below U+0020 become \u00xx in lowercase hex; everything else is written
//     as its own UTF-8 bytes. Nothing above U+007F is escaped.
//   - Invalid UTF-8 is refused, never repaired.
//   - Numbers are integers, written as their shortest decimal form. A float is
//     refused: no two languages agree on how to print one, and nothing here
//     needs one.
//   - There is no whitespace anywhere.
//
// This is RFC 8785's shape for the parts Rokh uses, and it deliberately stops
// short of that document's number rules, which exist for floats.
package canon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	// ErrNotUTF8 is returned for a string that is not valid UTF-8.
	ErrNotUTF8 = errors.New("canon: a string must be valid UTF-8")
	// ErrFloat is returned for a non-integer number.
	ErrFloat = errors.New("canon: a float has no agreed spelling; use an integer")
	// ErrNotCanonical is returned when bytes are not the one encoding of what
	// they decode to.
	ErrNotCanonical = errors.New("canon: not the canonical encoding")
)

// Marshal writes a value in the byte order above.
//
// The value goes through encoding/json first, to get the struct tags and the
// omitempty rules, and is then written out again by the rules here. So a change
// to Go's escaping cannot reach the bytes, and neither can the field order of a
// struct: what comes out is sorted, and stays sorted when somebody reorders a
// declaration.
func Marshal(v any) ([]byte, error) {
	// Before encoding/json, not after: it repairs invalid UTF-8 into U+FFFD
	// without a word, and by the time the bytes are here the two different
	// inputs already share a name.
	if err := validate(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Canon(raw)
}

// Canon rewrites well-formed JSON in the canonical byte order.
func Canon(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("canon: trailing bytes after the value")
	}
	var b strings.Builder
	if err := write(&b, v); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// Check enforces the law: these bytes are the one encoding of what they say.
// A second spelling is refused, not repaired.
func Check(raw []byte) error {
	got, err := Canon(raw)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, raw) {
		return ErrNotCanonical
	}
	return nil
}

func write(b *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		return writeString(b, t)
	case json.Number:
		s := t.String()
		if strings.ContainsAny(s, ".eE") {
			return fmt.Errorf("%w: %s", ErrFloat, s)
		}
		b.WriteString(s)
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := write(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeString(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := write(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("canon: cannot write %T", v)
	}
	return nil
}

const hexDigits = "0123456789abcdef"

func writeString(b *strings.Builder, s string) error {
	if !utf8.ValidString(s) {
		return ErrNotUTF8
	}
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
				continue
			}
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return nil
}

// validate walks a value and refuses any string that is not valid UTF-8,
// before encoding/json gets a chance to repair it.
//
// The repair is the problem. json.Marshal replaces a bad byte with U+FFFD and
// says nothing, so by the time bytes reach the writer above they are already
// valid — and two different inputs have been given one name. The only place to
// catch it is here, in front.
func validate(v reflect.Value) error {
	if !v.IsValid() {
		return nil
	}
	// A nil pointer or interface has nothing to walk, and asking one for its
	// JSON calls a value method through nothing. Checked before the Marshaler
	// question, not after.
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
	}
	// A type that writes its own JSON is asked for it and checked, since it
	// may emit anything at all.
	if v.CanInterface() {
		if m, ok := v.Interface().(json.Marshaler); ok {
			b, err := m.MarshalJSON()
			if err != nil {
				return err
			}
			if !utf8.Valid(b) {
				return ErrNotUTF8
			}
			return nil
		}
	}
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return ErrNotUTF8
		}
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return validate(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := validate(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if err := validate(k); err != nil {
				return err
			}
			if err := validate(v.MapIndex(k)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			if err := validate(v.Field(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Strict reads bytes into a value and enforces that they are the one encoding
// of exactly that value.
//
// It exists because encoding/json is lenient in two ways that are Go's
// leniency and not JSON's, and both of them open the door this law exists to
// shut — two byte strings, one meaning, two names:
//
//   - It matches field names case-insensitively. A payload spelled {"TYPE":…}
//     unmarshals into a field tagged "type", so a different byte string is
//     read as the same thing. Nothing about JSON says that.
//   - It takes the last of duplicate keys without a word.
//
// Neither can be turned off, and DisallowUnknownFields does not help: "TYPE"
// is not unknown to Go, it is a match. So the check is structural. What was
// decoded is written back out in the one byte order, and if that is not
// exactly what arrived, these bytes were not the encoding of this value.
//
//	— N4.1, T3.1
func Strict(raw []byte, v any) error {
	if err := Check(raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return err
	}
	// Dereference so that a pointer target re-encodes as its value.
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return fmt.Errorf("canon: nowhere to read into")
		}
		rv = rv.Elem()
	}
	again, err := Marshal(rv.Interface())
	if err != nil {
		return err
	}
	if !bytes.Equal(again, raw) {
		return ErrNotCanonical
	}
	return nil
}

// Peek reads one top-level string field by its exact key, without decoding the
// rest and without Go's case-insensitive matching.
//
// Recognising an artefact must not require parsing all of it: a reader decides
// whether a payload is its business before it is willing to read it as its
// own. But the peek has to be as exact as the full read, or "TYPE" would get
// past the door that "type" was checked at.
func Peek(raw []byte, key string) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, err := dec.Token()
	if err != nil {
		return "", false
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return "", false
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return "", false
		}
		k, ok := kt.(string)
		if !ok {
			return "", false
		}
		if k != key {
			// Skip the value, whatever shape it is.
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return "", false
			}
			continue
		}
		var val any
		if err := dec.Decode(&val); err != nil {
			return "", false
		}
		s, ok := val.(string)
		return s, ok
	}
	return "", false
}
