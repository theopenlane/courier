package controlfile

import (
	"github.com/goccy/go-yaml"
)

// yamlIndent is the indentation width for rendered YAML
const yamlIndent = 2

// yamlOptions render nested sequences indented under their key
var yamlOptions = []yaml.EncodeOption{yaml.Indent(yamlIndent), yaml.IndentSequence(true)}

// Marshal serializes a document list to YAML without modifying the data
func Marshal[T any](items []*T) ([]byte, error) {
	return yaml.MarshalWithOptions(items, yamlOptions...)
}

// Unmarshal parses a YAML document list without validating it
func Unmarshal[T any](data []byte) ([]*T, error) {
	var items []*T
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, err
	}

	return items, nil
}
