package convert

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/theopenlane/courier/pkg/controlfile"
)

const (
	// csvListSeparator separates multiple values within a single cell, a pipe
	// keeps lists readable without quoting them against the field separator
	csvListSeparator = "|"

	// csvKeySeparator separates a map field from the key it fills, e.g. the
	// mappedControls.SOC 2 column fills the SOC 2 key of mappedControls
	csvKeySeparator = "."

	// csvParentPrefix marks the column naming the parent of a nested record,
	// either alone or before the key field, e.g. parent or parentRefCode
	csvParentPrefix = "parent"

	// CSVSkipColumn maps a column to no field, dropping it from the output
	CSVSkipColumn = skipMarker
)

// CSVOptions configures how the cells of a CSV map onto document fields
type CSVOptions struct {
	// Columns maps a header cell to the document field it fills, taking
	// precedence over the field the header itself names. Keys and values are
	// matched the same way as headers, so "control owner" maps as
	// "controlOwner" does, and a value of CSVSkipColumn drops the column
	Columns map[string]string
	// SkipUnknownColumns drops columns naming no document field rather than
	// failing on them
	SkipUnknownColumns bool
	// KeyField is the field identifying a record, used to detect duplicates
	// and to attach nested records to their parent, it defaults to the field
	// the schema marks required, e.g. refCode for a control
	KeyField string
}

// CSV parses a CSV into the documents of any store type, T is the document
// struct, e.g. Control or Policy, and its fields determine what the CSV may
// hold, the result is validated against the schema reflected from T.
//
// The first record is a header naming the fields to fill. Header cells are
// case insensitive and may be separated by spaces, dashes, or underscores, so
// refCode, ref_code, and "Ref Code" all name the same field. A column named
// <field>.<key> fills one key of a map field, e.g. mappedControls.SOC 2 on a
// control or satisfies.SOC 2 on policy frontmatter. Multiple values in one
// cell, for list fields and map values, separate with a pipe.
//
// When T nests records of another type, as a control nests subcontrols, a
// column named parent, or parent<KeyField>, attaches the row to the record
// with that key, which must appear in an earlier row.
//
// Empty cells are left unset rather than written as empty values, and unknown
// columns are an error so a misspelled header is not silently dropped, opts
// maps or skips the columns of a CSV that does not already name the fields
func CSV[T any](r io.Reader, opts CSVOptions) ([]*T, error) {
	desc, err := describeType(reflect.TypeFor[T](), opts.KeyField)
	if err != nil {
		return nil, err
	}

	rows, err := readCSVRows(r, desc, opts)
	if err != nil {
		return nil, err
	}

	var documents []*T

	index := map[string]reflect.Value{}

	for _, row := range rows {
		if row.parent == "" {
			document := new(T)

			value := reflect.ValueOf(document).Elem()
			if err := row.fill(value, desc); err != nil {
				return nil, err
			}

			key, err := row.key(value, desc)
			if err != nil {
				return nil, err
			}

			if key != "" {
				if _, ok := index[key]; ok {
					return nil, fmt.Errorf("%w: %s on line %d", ErrDuplicateKey, key, row.line)
				}

				index[key] = value
			}

			documents = append(documents, document)

			continue
		}

		parent, ok := index[row.parent]
		if !ok {
			return nil, fmt.Errorf("%w: %s on line %d", ErrCSVUnknownParent, row.parent, row.line)
		}

		if err := appendNested(parent, desc, row); err != nil {
			return nil, err
		}
	}

	if err := controlfile.Validate(documents); err != nil {
		return nil, err
	}

	return documents, nil
}

// CSVToYAML converts a CSV into the YAML content of the store file holding
// documents of type T, an empty input renders as an empty document
func CSVToYAML[T any](r io.Reader, opts CSVOptions) ([]byte, error) {
	documents, err := CSV[T](r, opts)
	if err != nil {
		return nil, err
	}

	return controlfile.Marshal(documents)
}

// key is the value identifying the record a row filled, a record that does
// not fill the key field its type is identified by cannot be matched
func (row csvRow) key(value reflect.Value, desc *docType) (string, error) {
	key := desc.recordKey(value)
	if key == "" && desc.key != "" {
		return "", fmt.Errorf("%w: %s on line %d", ErrMissingKey, desc.keyName, row.line)
	}

	return key, nil
}

// appendNested builds a nested record from a row and appends it to its parent
func appendNested(parent reflect.Value, desc *docType, row csvRow) error {
	nested := reflect.New(desc.nested.typ)

	if err := row.fill(nested.Elem(), desc.nested); err != nil {
		return err
	}

	if _, err := row.key(nested.Elem(), desc.nested); err != nil {
		return err
	}

	field := parent.Field(desc.nestedIndex)
	field.Set(reflect.Append(field, nested))

	return nil
}

// csvColumn is a parsed header cell, key is set for map columns and holds the
// map key verbatim
type csvColumn struct {
	field  string
	key    string
	parent bool
	skip   bool
}

// id identifies the column for duplicate detection
func (c csvColumn) id() string {
	return c.field + csvKeySeparator + c.key
}

// csvCell is one filled cell of a record
type csvCell struct {
	column csvColumn
	value  string
}

// csvRow is one parsed CSV record, parent is the key of the record the row
// nests under when the row describes a nested record
type csvRow struct {
	line   int
	parent string
	cells  []csvCell
}

// fill writes the cells of a row into a document value, the same row fills a
// document or a nested record depending on the type it is filled into
func (row csvRow) fill(value reflect.Value, desc *docType) error {
	for _, cell := range row.cells {
		field, ok := desc.fields[cell.column.field]
		if !ok {
			return fmt.Errorf("%w: %s on line %d", ErrCSVUnknownColumn, cell.column.field, row.line)
		}

		if err := setCSVField(value.Field(field.index), field, cell); err != nil {
			return fmt.Errorf("%w on line %d", err, row.line)
		}
	}

	return nil
}

// setCSVField writes one cell into the field its column names
func setCSVField(target reflect.Value, field docField, cell csvCell) error {
	switch field.kind {
	case scalarField:
		if cell.column.key != "" {
			return fmt.Errorf("%w: %s", ErrCSVNotAMap, cell.column.field)
		}

		return setCSVScalar(target, cell.value)
	case listField:
		if cell.column.key != "" {
			return fmt.Errorf("%w: %s", ErrCSVNotAMap, cell.column.field)
		}

		return setCSVList(target, cell.value)
	case mapField:
		if cell.column.key == "" {
			return fmt.Errorf("%w: %s", ErrCSVMissingMapKey, cell.column.field)
		}

		if target.IsNil() {
			target.Set(reflect.MakeMap(field.typ))
		}

		entry := reflect.New(field.typ.Elem()).Elem()

		if err := setCSVValue(entry, cell.value); err != nil {
			return err
		}

		target.SetMapIndex(reflect.ValueOf(cell.column.key).Convert(field.typ.Key()), entry)

		return nil
	default:
		return fmt.Errorf("%w: %s", ErrCSVUnknownColumn, cell.column.field)
	}
}

// setCSVValue writes a cell into a value of any fillable kind
func setCSVValue(target reflect.Value, value string) error {
	if target.Kind() == reflect.Slice {
		return setCSVList(target, value)
	}

	return setCSVScalar(target, value)
}

// setCSVList splits a cell into the elements of a list, empty entries are dropped
func setCSVList(target reflect.Value, value string) error {
	list := reflect.MakeSlice(target.Type(), 0, 1)

	for part := range strings.SplitSeq(value, csvListSeparator) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		element := reflect.New(target.Type().Elem()).Elem()

		if err := setCSVScalar(element, part); err != nil {
			return err
		}

		list = reflect.Append(list, element)
	}

	target.Set(list)

	return nil
}

// setCSVScalar parses a cell into a single value
func setCSVScalar(target reflect.Value, value string) error {
	switch target.Kind() {
	case reflect.String:
		target.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%w: %s is not a boolean", ErrCSVInvalidValue, value)
		}

		target.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("%w: %s is not an integer", ErrCSVInvalidValue, value)
		}

		target.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("%w: %s is not an unsigned integer", ErrCSVInvalidValue, value)
		}

		target.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("%w: %s is not a number", ErrCSVInvalidValue, value)
		}

		target.SetFloat(parsed)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedType, target.Type())
	}

	return nil
}

// readCSVRows reads the header and every data record, blank records are
// skipped so trailing newlines in hand-edited files do not become entries
func readCSVRows(r io.Reader, desc *docType, opts CSVOptions) ([]csvRow, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	// records shorter than the header simply leave the trailing fields unset
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, ErrCSVMissingHeader
	}

	columns, err := parseCSVHeader(records[0], desc, opts)
	if err != nil {
		return nil, err
	}

	var rows []csvRow

	for i, record := range records[1:] {
		// the header occupies the first record, so data starts on line two
		line := i + 2 //nolint:mnd

		if blankRecord(record) {
			continue
		}

		row, err := parseCSVRecord(columns, record, line)
		if err != nil {
			return nil, err
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// parseCSVHeader resolves every header cell to the field it fills, rejecting
// unknown and repeated columns
func parseCSVHeader(record []string, desc *docType, opts CSVOptions) ([]csvColumn, error) {
	aliases := map[string]string{}
	for header, field := range opts.Columns {
		aliases[normalizeName(header)] = field
	}

	columns := make([]csvColumn, 0, len(record))
	seen := map[string]bool{}

	for _, cell := range record {
		name := cell
		if alias, ok := aliases[normalizeName(cell)]; ok {
			name = alias
		}

		column, err := parseCSVColumn(name, desc)
		if err != nil {
			if opts.SkipUnknownColumns {
				columns = append(columns, csvColumn{skip: true})

				continue
			}

			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(cell))
		}

		if column.skip {
			columns = append(columns, column)

			continue
		}

		if seen[column.id()] {
			return nil, fmt.Errorf("%w: %s", ErrCSVDuplicateColumn, strings.TrimSpace(cell))
		}

		seen[column.id()] = true

		columns = append(columns, column)
	}

	return columns, nil
}

// parseCSVColumn resolves a single header cell, or its alias, to a field of
// the document type or of the records it nests
func parseCSVColumn(cell string, desc *docType) (csvColumn, error) {
	if strings.TrimSpace(cell) == CSVSkipColumn {
		return csvColumn{skip: true}, nil
	}

	name, key, mapped := strings.Cut(cell, csvKeySeparator)

	name = normalizeName(name)
	key = strings.TrimSpace(key)

	if mapped && key == "" {
		return csvColumn{}, ErrCSVMissingMapKey
	}

	if !mapped && desc.nested != nil && isParentColumn(name, desc) {
		return csvColumn{parent: true}, nil
	}

	if _, ok := desc.fields[name]; !ok {
		if desc.nested == nil {
			return csvColumn{}, ErrCSVUnknownColumn
		}

		if _, ok := desc.nested.fields[name]; !ok {
			return csvColumn{}, ErrCSVUnknownColumn
		}
	}

	return csvColumn{field: name, key: key}, nil
}

// isParentColumn reports whether a column names the parent of a nested
// record, either as parent or as the prefix before the key field
func isParentColumn(name string, desc *docType) bool {
	return name == csvParentPrefix || (desc.key != "" && name == csvParentPrefix+desc.key)
}

// parseCSVRecord collects the filled cells of one data record
func parseCSVRecord(columns []csvColumn, record []string, line int) (csvRow, error) {
	if len(record) > len(columns) {
		return csvRow{}, fmt.Errorf("%w: line %d has %d fields, header has %d", ErrCSVFieldCount, line, len(record), len(columns))
	}

	row := csvRow{line: line}

	for i, value := range record {
		value = strings.TrimSpace(value)
		if value == "" || columns[i].skip {
			continue
		}

		if columns[i].parent {
			row.parent = value

			continue
		}

		row.cells = append(row.cells, csvCell{column: columns[i], value: value})
	}

	return row, nil
}

// blankRecord reports whether every cell in a record is empty
func blankRecord(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}

	return true
}
