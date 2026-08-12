package convert

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/theopenlane/courier/pkg/controlfile"
)

// pathSeparator joins a record key to the field it names in a merge report
const pathSeparator = "."

// MergeOptions configures how two document sets combine
type MergeOptions struct {
	// KeyField is the field records are matched by, it defaults to the field
	// the schema marks required, e.g. refCode for a control
	KeyField string
	// SkipNewRecords leaves out records the secondary set holds and the
	// primary does not, rather than appending them
	SkipNewRecords bool
	// IDField is the field holding the id the server assigned a record, which
	// records match on before they match on their key, it defaults to the id
	// field of the type and MatchKeyOnly turns the id matching off
	IDField string
	// MatchKeyOnly matches records by their key alone, ignoring server
	// assigned ids
	MatchKeyOnly bool
}

// MergeReport lists what a merge took from the secondary set
type MergeReport struct {
	// Filled are the fields the secondary set filled in, as key.field
	Filled []string
	// Added are the keys of the records the secondary set contributed whole
	Added []string
	// Skipped are the keys of the records left out by SkipNewRecords
	Skipped []string
}

// Merge combines two document sets into one, the primary set wins: a field it
// fills is never replaced, and a field it leaves empty takes the value the
// secondary set holds for the same record. Map fields, e.g. the mappedControls
// of a control, merge key by key so the secondary set contributes only the
// frameworks the primary set is missing.
//
// Records match the way apply matches them against Openlane: on the id the
// server assigned when both records carry one, and on their key, e.g. the
// refCode of a control, otherwise. Matching on the id first means a record
// renamed in Openlane still merges with the record it came from rather than
// reading as a new one.
//
// Records the secondary set holds and the primary does not are appended, in
// their own order, unless SkipNewRecords is set. Nested records, e.g. the
// subcontrols of a control, merge under their parent by the same rules.
//
// The primary records are filled in place and the merged set holds them, so
// the caller's primary documents come back merged, the result is validated
// against the schema before it is returned
func Merge[T any](primary, secondary []*T, opts MergeOptions) ([]*T, MergeReport, error) {
	desc, err := describeType(reflect.TypeFor[T](), opts.KeyField)
	if err != nil {
		return nil, MergeReport{}, err
	}

	if desc.key == "" {
		return nil, MergeReport{}, fmt.Errorf("%w: %s", ErrNoKeyField, desc.typ)
	}

	if err := applyIDField(desc, opts); err != nil {
		return nil, MergeReport{}, err
	}

	merged := slices.Clone(primary)

	index := newIndex(desc)

	for _, document := range merged {
		if err := index.add(reflect.ValueOf(document).Elem()); err != nil {
			return nil, MergeReport{}, err
		}
	}

	report := MergeReport{}

	for _, document := range secondary {
		value := reflect.ValueOf(document).Elem()

		key := desc.recordKey(value)
		if key == "" {
			return nil, MergeReport{}, fmt.Errorf("%w: %s", ErrMissingKey, desc.keyName)
		}

		target, ok := index.match(value)
		if !ok {
			if opts.SkipNewRecords {
				report.Skipped = append(report.Skipped, key)

				continue
			}

			merged = append(merged, document)

			if err := index.add(value); err != nil {
				return nil, MergeReport{}, err
			}

			report.Added = append(report.Added, key)

			continue
		}

		mergeRecord(target, value, desc, desc.recordKey(target), &report)
	}

	if err := controlfile.Validate(merged); err != nil {
		return nil, MergeReport{}, err
	}

	return merged, report, nil
}

// MergeYAML combines two store files holding documents of type T and renders
// the result, the primary file wins field by field
func MergeYAML[T any](primary, secondary []byte, opts MergeOptions) ([]byte, MergeReport, error) {
	primaryDocs, err := controlfile.Unmarshal[T](primary)
	if err != nil {
		return nil, MergeReport{}, err
	}

	secondaryDocs, err := controlfile.Unmarshal[T](secondary)
	if err != nil {
		return nil, MergeReport{}, err
	}

	merged, report, err := Merge(primaryDocs, secondaryDocs, opts)
	if err != nil {
		return nil, report, err
	}

	data, err := controlfile.Marshal(merged)

	return data, report, err
}

// recordIndex matches the records of a set the way apply matches them against
// Openlane: on the id the server assigned, and on the record key otherwise
type recordIndex struct {
	desc  *docType
	byID  map[string]reflect.Value
	byKey map[string]reflect.Value
}

// newIndex builds an empty index over a document type
func newIndex(desc *docType) *recordIndex {
	return &recordIndex{desc: desc, byID: map[string]reflect.Value{}, byKey: map[string]reflect.Value{}}
}

// add indexes a record, rejecting a set that identifies two records the same
// way since a merge could not tell them apart
func (r *recordIndex) add(value reflect.Value) error {
	key := r.desc.recordKey(value)
	if key == "" {
		return fmt.Errorf("%w: %s", ErrMissingKey, r.desc.keyName)
	}

	if _, ok := r.byKey[key]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateKey, key)
	}

	r.byKey[key] = value

	id := r.desc.recordID(value)
	if id == "" {
		return nil
	}

	if _, ok := r.byID[id]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateID, id)
	}

	r.byID[id] = value

	return nil
}

// match finds the record a secondary record merges into, an id both records
// carry wins over their keys, so a record renamed in Openlane still matches
func (r *recordIndex) match(value reflect.Value) (reflect.Value, bool) {
	if id := r.desc.recordID(value); id != "" {
		if target, ok := r.byID[id]; ok {
			return target, true
		}
	}

	target, ok := r.byKey[r.desc.recordKey(value)]

	return target, ok
}

// applyIDField resolves the field records match on before their key, an empty
// IDField leaves the id field of the type in place
func applyIDField(desc *docType, opts MergeOptions) error {
	if opts.MatchKeyOnly {
		desc.id = ""

		return nil
	}

	if opts.IDField == "" {
		return nil
	}

	name := normalizeName(opts.IDField)
	if _, ok := desc.fields[name]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownIDField, opts.IDField)
	}

	desc.id = name

	if desc.nested != nil {
		return applyIDField(desc.nested, opts)
	}

	return nil
}

// mergeRecord fills the empty fields of a primary record from a secondary one
func mergeRecord(primary, secondary reflect.Value, desc *docType, path string, report *MergeReport) {
	for i := range primary.NumField() {
		if !primary.Field(i).CanSet() {
			continue
		}

		if i == desc.nestedIndex {
			mergeNested(primary.Field(i), secondary.Field(i), desc.nested, path, report)

			continue
		}

		from, to := secondary.Field(i), primary.Field(i)
		if from.IsZero() {
			continue
		}

		name := fieldPath(path, desc, i)

		if to.Kind() == reflect.Map {
			mergeMap(to, from, name, report)

			continue
		}

		if !to.IsZero() {
			continue
		}

		to.Set(from)

		report.Filled = append(report.Filled, name)
	}
}

// mergeMap fills the keys of a map field the primary record does not hold,
// a key it already holds keeps its own value
func mergeMap(primary, secondary reflect.Value, path string, report *MergeReport) {
	for _, key := range secondary.MapKeys() {
		if primary.MapIndex(key).IsValid() {
			continue
		}

		if primary.IsNil() {
			primary.Set(reflect.MakeMap(primary.Type()))
		}

		primary.SetMapIndex(key, secondary.MapIndex(key))

		report.Filled = append(report.Filled, path+pathSeparator+key.String())
	}
}

// mergeNested merges the records nested under a pair of matched records,
// nested records the primary does not hold are appended
func mergeNested(primary, secondary reflect.Value, desc *docType, path string, report *MergeReport) {
	if desc == nil || secondary.Len() == 0 {
		return
	}

	index := newIndex(desc)

	for i := range primary.Len() {
		// a nested record the primary cannot identify simply matches nothing
		_ = index.add(primary.Index(i).Elem())
	}

	for i := range secondary.Len() {
		nested := secondary.Index(i)

		target, ok := index.match(nested.Elem())
		if !ok {
			primary.Set(reflect.Append(primary, nested))
			report.Added = append(report.Added, path+pathSeparator+desc.recordKey(nested.Elem()))

			continue
		}

		mergeRecord(target, nested.Elem(), desc, path+pathSeparator+desc.recordKey(target), report)
	}
}

// fieldPath names a field of a record for the merge report
func fieldPath(path string, desc *docType, index int) string {
	for _, field := range desc.fields {
		if field.index == index {
			return path + pathSeparator + field.name
		}
	}

	return path + pathSeparator + desc.typ.Field(index).Name
}
