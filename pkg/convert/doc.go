// Package convert turns external inputs into the store files courier keeps in
// git. Conversion is driven by the document type itself: the fields of the Go
// struct, and the schema reflected from it, decide what an input may hold, so
// a new document kind converts without new conversion code. Output goes
// through the same marshaling as an export, making a converted file
// indistinguishable from a pulled one
package convert
