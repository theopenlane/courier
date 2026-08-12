package convert

import (
	"fmt"
	"reflect"
	"strings"
)

const (
	// yamlTag names the struct tag the store files are written from
	yamlTag = "yaml"

	// schemaTag names the struct tag the document schemas are reflected from
	schemaTag = "jsonschema"

	// requiredTag marks the field a record is identified by
	requiredTag = "required"

	// skipMarker excludes a field or column, matching the yaml tag that
	// keeps a field out of the store files
	skipMarker = "-"

	// nameSeparators are dropped from field and column names so refCode,
	// ref_code, and "Ref Code" all name the same field
	nameSeparators = " _-"

	// idFieldName is the field holding the id the server assigned a record,
	// records match on it before they match on their key
	idFieldName = "id"
)

// docKind is how a cell fills the field its column names
type docKind int

const (
	// scalarField fills the field with the cell itself
	scalarField docKind = iota
	// listField splits the cell into the elements of a list field
	listField
	// mapField splits the cell into the values held under one key of a map field
	mapField
)

// docField is a document field a column can fill
type docField struct {
	// name is the field as the yaml tag names it
	name  string
	index int
	kind  docKind
	typ   reflect.Type
}

// docType describes how the fields of a document type map onto CSV columns
type docType struct {
	typ reflect.Type
	// fields are the fillable fields by normalized name
	fields map[string]docField
	// key is the normalized name of the field identifying a record
	key string
	// keyName is the key field as the schema names it, for error messages
	keyName string
	// id is the normalized name of the field holding the server assigned id
	// of a record, empty when the type carries none
	id string
	// nested describes the records this type nests, nil when it nests none
	nested *docType
	// nestedIndex is the field index of the nested record list
	nestedIndex int
}

// describeType reflects the fields of a document type from the yaml tags that
// name them in the store files, keyField overrides the field a record is
// identified by, which defaults to the field the schema marks required
func describeType(typ reflect.Type, keyField string) (*docType, error) {
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedType, typ)
	}

	desc := &docType{typ: typ, fields: map[string]docField{}, nestedIndex: -1}
	wanted := normalizeName(keyField)

	for i := range typ.NumField() {
		field := typ.Field(i)

		name := yamlFieldName(field)
		if name == "" {
			continue
		}

		if nested := nestedStruct(field.Type); nested != nil {
			// records nest one level, only the first nested list is fillable
			if desc.nested == nil && nested != typ {
				child, err := describeType(nested, keyField)
				if err != nil {
					return nil, err
				}

				desc.nested, desc.nestedIndex = child, i
			}

			continue
		}

		kind, ok := kindOf(field.Type)
		if !ok {
			continue
		}

		normalized := normalizeName(name)
		desc.fields[normalized] = docField{name: name, index: i, kind: kind, typ: field.Type}

		if isKeyField(field, normalized, wanted, desc) {
			desc.key, desc.keyName = normalized, name
		}

		if normalized == idFieldName && field.Type.Kind() == reflect.String {
			desc.id = normalized
		}
	}

	if wanted != "" && desc.key == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKeyField, keyField)
	}

	return desc, nil
}

// isKeyField reports whether a field identifies a record, the requested key
// wins over the field the schema marks required
func isKeyField(field reflect.StructField, normalized, wanted string, desc *docType) bool {
	if field.Type.Kind() != reflect.String {
		return false
	}

	if wanted != "" {
		return normalized == wanted
	}

	return desc.key == "" && strings.Contains(field.Tag.Get(schemaTag), requiredTag)
}

// nestedStruct returns the struct type a field nests, for a field holding a
// list of struct pointers, and nil for every other field
func nestedStruct(typ reflect.Type) reflect.Type {
	if typ.Kind() != reflect.Slice || typ.Elem().Kind() != reflect.Pointer {
		return nil
	}

	if typ.Elem().Elem().Kind() != reflect.Struct {
		return nil
	}

	return typ.Elem().Elem()
}

// kindOf resolves how a cell fills a field, unsupported field types are
// not fillable and their columns are unknown
func kindOf(typ reflect.Type) (docKind, bool) {
	switch typ.Kind() {
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return 0, false
		}

		if _, ok := kindOf(typ.Elem()); !ok {
			return 0, false
		}

		return mapField, true
	case reflect.Slice:
		if _, ok := kindOf(typ.Elem()); !ok {
			return 0, false
		}

		return listField, true
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return scalarField, true
	default:
		return 0, false
	}
}

// yamlFieldName is the name the yaml tag gives an exported field, unnamed and
// skipped fields report an empty name
func yamlFieldName(field reflect.StructField) string {
	if !field.IsExported() {
		return ""
	}

	name, _, _ := strings.Cut(field.Tag.Get(yamlTag), ",")
	if name == skipMarker {
		return ""
	}

	if name == "" {
		return field.Name
	}

	return name
}

// recordKey returns the value identifying a record, a type whose schema marks
// no field required has no key and every record reports an empty one
func (d *docType) recordKey(value reflect.Value) string {
	if d.key == "" {
		return ""
	}

	return value.Field(d.fields[d.key].index).String()
}

// recordID returns the id the server assigned a record, a type carrying no id
// field, and a record the server has never seen, both report an empty one
func (d *docType) recordID(value reflect.Value) string {
	if d.id == "" {
		return ""
	}

	return value.Field(d.fields[d.id].index).String()
}

// normalizeName lowercases a field or column name and drops the separators
// used between words so refCode, ref_code, and "Ref Code" name the same field
func normalizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(nameSeparators, r) {
			return -1
		}

		return r
	}, strings.ToLower(strings.TrimSpace(name)))
}
