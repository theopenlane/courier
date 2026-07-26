package controlfile

import (
	"github.com/goccy/go-yaml"
)

// marshalList serializes a list to YAML without modifying the data
func marshalList[T any](items []*T) ([]byte, error) {
	return yaml.Marshal(items)
}

// unmarshalList parses a YAML list without validating it
func unmarshalList[T any](data []byte) ([]*T, error) {
	var items []*T
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// MarshalControls serializes the control inventory
func MarshalControls(controls []*Control) ([]byte, error) {
	return marshalList(controls)
}

// UnmarshalControls parses a controls.yaml inventory without validating it
func UnmarshalControls(data []byte) ([]*Control, error) {
	return unmarshalList[Control](data)
}

// MarshalPolicies serializes the policy manifest
func MarshalPolicies(policies []*Policy) ([]byte, error) {
	return marshalList(policies)
}

// UnmarshalPolicies parses a policies.yaml manifest without validating it
func UnmarshalPolicies(data []byte) ([]*Policy, error) {
	return unmarshalList[Policy](data)
}
