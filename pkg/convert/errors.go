package convert

import "errors"

var (
	// ErrCSVMissingHeader is returned when a CSV input has no header record
	ErrCSVMissingHeader = errors.New("csv is missing a header record")

	// ErrCSVUnknownColumn is returned when a header cell does not name a document field
	ErrCSVUnknownColumn = errors.New("unknown csv column")

	// ErrCSVDuplicateColumn is returned when a header names the same field twice
	ErrCSVDuplicateColumn = errors.New("duplicate csv column")

	// ErrCSVMissingMapKey is returned when a column filling a map field does not
	// name the key it fills, e.g. mappedControls without a framework
	ErrCSVMissingMapKey = errors.New("csv column is missing a map key")

	// ErrCSVNotAMap is returned when a column names a key on a field that is not a map
	ErrCSVNotAMap = errors.New("csv column names a key on a field that is not a map")

	// ErrCSVFieldCount is returned when a record holds more fields than the header
	ErrCSVFieldCount = errors.New("csv record does not match the header")

	// ErrMissingKey is returned when a record does not fill the field
	// identifying it, e.g. a control without a refCode
	ErrMissingKey = errors.New("record is missing its key field")

	// ErrDuplicateKey is returned when two records carry the same key
	ErrDuplicateKey = errors.New("duplicate record key")

	// ErrCSVUnknownParent is returned when a nested record names a parent that
	// no earlier record declares
	ErrCSVUnknownParent = errors.New("csv parent does not match an earlier record")

	// ErrDuplicateID is returned when two records carry the same server
	// assigned id
	ErrDuplicateID = errors.New("duplicate record id")

	// ErrUnknownIDField is returned when the requested id field does not name
	// a field of the document type
	ErrUnknownIDField = errors.New("unknown id field")

	// ErrNoKeyField is returned when documents of a type cannot be matched
	// because the type has no field identifying a record
	ErrNoKeyField = errors.New("document type has no key field")

	// ErrUnknownKeyField is returned when the requested key field does not
	// name a field of the document type
	ErrUnknownKeyField = errors.New("unknown key field")

	// ErrUnsupportedType is returned when a document type, or one of its
	// fields, cannot be filled from an input
	ErrUnsupportedType = errors.New("unsupported document type")

	// ErrCSVInvalidValue is returned when a cell does not parse as the type of
	// the field its column names
	ErrCSVInvalidValue = errors.New("invalid csv value")

	// ErrUnknownFormat is returned when an input file has no known converter
	ErrUnknownFormat = errors.New("unknown input format")

	// ErrUnknownKind is returned when a target document kind is not registered
	ErrUnknownKind = errors.New("unknown document kind")
)
