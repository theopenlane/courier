package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/mcuadros/go-defaults"
	"github.com/theopenlane/core/pkg/jsonx"
	"gopkg.in/yaml.v3"

	"github.com/theopenlane/courier/pkg/controlfile"
	"github.com/theopenlane/courier/pkg/engine"
)

const (
	tagName            = "koanf"
	jsonSchemaPath     = "./schema/courier.config.json"
	controlsSchemaPath = "./schema/controls.json"
	policiesSchemaPath = "./schema/policies.json"
	yamlConfigPath     = "./config/config.example.yaml"
	envConfigPath      = "./config/.env.example"
	envPrefix          = "COURIER_"
	ownerReadWrite     = 0o600
	yamlIndentSpaces   = 4
)

// main reflects the settings and document types into the schema artifacts
func main() {
	r := jsonschema.Reflector{
		ExpandedStruct:             true,
		FieldNameTag:               tagName,
		RequiredFromJSONSchemaTags: true,
	}

	if err := r.AddGoComments("github.com/theopenlane/courier/", "./pkg/engine"); err != nil {
		panic(err)
	}

	schema := r.Reflect(&engine.Settings{})

	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(jsonSchemaPath, data, ownerReadWrite); err != nil {
		panic(err)
	}

	settings := &engine.Settings{}
	defaults.SetDefaults(settings)

	entries := settingsEntries(settings, descriptions(schema))

	if err := writeYAMLExample(entries); err != nil {
		panic(err)
	}

	if err := writeEnvExample(entries); err != nil {
		panic(err)
	}

	if err := writeDocumentSchema[[]*controlfile.Control](controlsSchemaPath); err != nil {
		panic(err)
	}

	if err := writeDocumentSchema[[]*controlfile.Policy](policiesSchemaPath); err != nil {
		panic(err)
	}
}

// writeDocumentSchema writes the JSON schema for a store document type,
// reflected through the same jsonx reflector used for runtime validation so
// the published schema matches what apply enforces
func writeDocumentSchema[T any](path string) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, jsonx.SchemaFrom[T](), "", "  "); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), ownerReadWrite)
}

// entry is one configuration key with its default value and description
type entry struct {
	key         string
	value       string
	description string
}

// descriptions maps property names to their schema descriptions
func descriptions(schema *jsonschema.Schema) map[string]string {
	out := map[string]string{}

	for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
		out[pair.Key] = pair.Value.Description
	}

	return out
}

// settingsEntries walks the settings struct in field order and pairs each
// koanf key with its stubbed default and description
func settingsEntries(settings *engine.Settings, descs map[string]string) []entry {
	var entries []entry

	t := reflect.TypeOf(*settings)
	v := reflect.ValueOf(*settings)

	for i := range t.NumField() {
		key := t.Field(i).Tag.Get(tagName)
		if key == "" || key == "-" {
			continue
		}

		entries = append(entries, entry{
			key:         key,
			value:       v.Field(i).String(),
			description: descs[key],
		})
	}

	return entries
}

// writeYAMLExample renders the commented example configuration file
func writeYAMLExample(entries []entry) error {
	root := &yaml.Node{Kind: yaml.MappingNode}
	root.HeadComment = "courier configuration. Comments describe each key; edit values as needed."

	for _, e := range entries {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: e.key, HeadComment: e.description}
		valueNode := &yaml.Node{Kind: yaml.ScalarNode, Value: e.value}

		if e.value == "" {
			valueNode.Style = yaml.DoubleQuotedStyle
		}

		root.Content = append(root.Content, keyNode, valueNode)
	}

	var buf strings.Builder

	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(yamlIndentSpaces)

	if err := encoder.Encode(root); err != nil {
		return err
	}

	if err := encoder.Close(); err != nil {
		return err
	}

	return os.WriteFile(yamlConfigPath, []byte(buf.String()), ownerReadWrite)
}

// writeEnvExample renders the example environment file, keys mirror the
// COURIER_-prefixed overrides accepted at runtime
func writeEnvExample(entries []entry) error {
	var buf strings.Builder

	buf.WriteString("# courier environment variable overrides, values shown are the defaults\n")

	for _, e := range entries {
		envKey := envPrefix + strings.ToUpper(strings.ReplaceAll(e.key, "-", "_"))
		fmt.Fprintf(&buf, "%s=%q\n", envKey, e.value)
	}

	return os.WriteFile(envConfigPath, []byte(buf.String()), ownerReadWrite)
}
