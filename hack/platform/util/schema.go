package util

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	pluralize "github.com/gertd/go-pluralize"
	"github.com/ghodss/yaml"
	"github.com/invopop/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prefixSeparator = "/"
	anchorSeparator = "-"
)

var DefaultRequire = true

const BasePath = "platform/api/_partials/resources/"

const BaseResourcesPath = "platform/api/resources"

var pluralizeClient = pluralize.NewClient()

var commentMap map[string]string

type ObjectInformation struct {
	Title       string
	Name        string
	Plural      string
	Resource    string
	Description string
	File        string

	Object client.Object

	Project bool

	Retrieve bool
	Create   bool
	Update   bool
	Delete   bool

	SubResource                  string
	SubResourceCreate            bool
	SubResourceCreateDescription string
	SubResourceGet               bool
	SubResourceGetDescription    string
}

func GenerateSchema(configInstance interface{}) *jsonschema.Schema {
	return generateSchema(configInstance)
}

func generateSchema(configInstance interface{}) *jsonschema.Schema {
	r := new(jsonschema.Reflector)
	r.AllowAdditionalProperties = true
	r.RequiredFromJSONSchemaTags = true
	r.ExpandedStruct = true

	// add comments
	if commentMap == nil {
		commentMap = map[string]string{}

		runInDir("vendor", func() {
			err := jsonschema.ExtractGoComments("", "github.com/loft-sh/api/v4/pkg/apis/management/v1", commentMap)
			if err != nil {
				panic(err)
			}

			err = jsonschema.ExtractGoComments("", "github.com/loft-sh/api/v4/pkg/apis/storage/v1", commentMap)
			if err != nil {
				panic(err)
			}

			err = jsonschema.ExtractGoComments("", "github.com/loft-sh/api/v4/pkg/apis/ui/v1", commentMap)
			if err != nil {
				panic(err)
			}

			err = jsonschema.ExtractGoComments("", "github.com/loft-sh/agentapi/v4/pkg/apis/loft/cluster/v1", commentMap)
			if err != nil {
				panic(err)
			}

			err = jsonschema.ExtractGoComments("", "github.com/loft-sh/agentapi/v4/pkg/apis/loft/storage/v1", commentMap)
			if err != nil {
				panic(err)
			}
		})

		runInDir("vendor", func() {
			err := jsonschema.ExtractGoComments("", "k8s.io/apimachinery/pkg/apis/meta/v1", commentMap)
			if err != nil {
				panic(err)
			}
		})

		runInDir("vendor", func() {
			err := jsonschema.ExtractGoComments("", "k8s.io/api/core/v1", commentMap)
			if err != nil {
				panic(err)
			}
		})
	}
	r.CommentMap = commentMap

	// filter comments with <>
	for k := range r.CommentMap {
		r.CommentMap[k] = strings.ReplaceAll(r.CommentMap[k], "<", "(")
		r.CommentMap[k] = strings.ReplaceAll(r.CommentMap[k], ">", ")")
	}

	return r.Reflect(configInstance)
}

func runInDir(dir string, fn func()) {
	oldDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		panic(err)
	}

	err = os.Chdir(dir)
	if err != nil {
		panic(err)
	}

	defer func() {
		err = os.Chdir(oldDir)
		if err != nil {
			panic(err)
		}
	}()

	fn()
}

func GenerateMetadata(information *ObjectInformation) {
	schema := generateSchema(information.Object.DeepCopyObject())
	createSections(path.Join(BasePath, "metadata", "reference.mdx"), schema, schema.Definitions, false, false, true, 1)
}

func GenerateSection(object interface{}, skipMetadata bool, filePath string) {
	schema := generateSchema(object)
	createSections(filePath, schema, schema.Definitions, skipMetadata, false, false, 1)
}

func GenerateObjectOverview(information *ObjectInformation) {
	if information.Title == "" {
		information.Title = information.Name
	}
	if information.Plural == "" {
		information.Plural = information.Name + "s"
	}
	namespace := ""
	if !information.Project {
		namespace = information.Object.GetNamespace()
	}

	exampleName := "my-object"
	if information.Object.GetName() != "" {
		exampleName = information.Object.GetName()
	}

	basePath := path.Join(BasePath, information.Resource) + "/"
	if information.SubResource != "" {
		basePath = path.Join(basePath, information.SubResource) + "/"
	}

	schema := generateSchema(information.Object.DeepCopyObject())
	err := GenerateResource(schema, basePath, information.SubResource != "")
	if err != nil {
		panic(err)
	}

	// create the yaml object
	out, err := yaml.Marshal(information.Object)
	if err != nil {
		panic(err)
	}

	// create "Retrieve Object" partial
	if information.Retrieve {
		writeTemplate(TemplateRetrieveObject, path.Join(basePath, "retrieve.mdx"), RetrieveValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Plural:      information.Plural,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
		})
	}

	// create "Create Object" partial
	if information.Create {
		writeTemplate(TemplateCreateObject, path.Join(basePath, "create.mdx"), CreateValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
			YAMLObject:  string(out),
		})
	}

	// create "Update Object" partial
	if information.Update {
		updateObject := information.Object.DeepCopyObject().(client.Object)
		updateObject.SetCreationTimestamp(metav1.NewTime(time.Date(2023, 4, 3, 0, 0, 0, 0, time.UTC)))
		updateObject.SetGeneration(12)
		updateObject.SetUID("af5f9f0f-8ab9-4b4b-a595-a95a5921f3c2")
		updateObject.SetResourceVersion("66325905")
		out, _ := yaml.Marshal(updateObject)
		writeTemplate(TemplateUpdateObject, path.Join(basePath, "update.mdx"), UpdateValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Plural:      information.Plural,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
			YAMLObject:  string(out),
		})
	}

	// create "Delete Object" partial
	if information.Delete {
		writeTemplate(TemplateDeleteObject, path.Join(basePath, "delete.mdx"), DeleteValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Plural:      information.Plural,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
		})
	}

	// create SubResource Create partial
	if information.SubResourceCreate {
		writeTemplate(TemplateCreateSubResourceObject, path.Join(basePath, "subresourcecreate.mdx"), CreateSubResourceValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Description: information.SubResourceCreateDescription,
			SubResource: information.SubResource,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
			YAMLObject:  string(out),
		})
	}

	// create SubResource Get partial
	if information.SubResourceGet {
		writeTemplate(TemplateGetSubResourceObject, path.Join(basePath, "subresourceget.mdx"), GetSubResourceValues{
			Name:        information.Name,
			ExampleName: exampleName,
			Description: information.SubResourceGetDescription,
			SubResource: information.SubResource,
			Resource:    information.Resource,
			Project:     information.Project,
			Namespace:   namespace,
		})
	}

	relPath := ""
	dir, file := path.Split(information.File)
	for file != "api" {
		dir, file = path.Split(strings.TrimSuffix(dir, "/"))
		if file == "" {
			panic("Unsupported path: " + information.File)
		}

		if file != "api" {
			relPath += "../"
		}
	}
	if relPath == "" {
		relPath = "."
	}
	relPath = strings.TrimSuffix(relPath, "/")

	// write overview
	writeTemplate(TemplateObjectOverview, information.File, ObjectOverviewValues{
		Title:        information.Title,
		Resource:     information.Resource,
		Description:  information.Description,
		RelativePath: relPath,
		Name:         information.Name,
		Plural:       information.Plural,
		YAMLObject:   string(out),

		Project: information.Project,

		Create:   information.Create,
		Retrieve: information.Retrieve,
		Update:   information.Update,
		Delete:   information.Delete,

		SubResource:       information.SubResource,
		SubResourceCreate: information.SubResourceCreate,
		SubResourceGet:    information.SubResourceGet,
	})
}

func GenerateFromPath(schema *jsonschema.Schema, basePath string, schemaPath string, defaults map[string]interface{}) {
	err := GenerateFromPathWithError(schema, basePath, schemaPath, defaults)
	if err != nil {
		panic(err)
	}
}

func GenerateFromPathWithError(schema *jsonschema.Schema, basePath string, schemaPath string, defaults map[string]interface{}) error {
	splittedSchemaPath := strings.Split(schemaPath, "/")

	fieldSchema := schema
	lastProperty := schemaPath
	for i, property := range splittedSchemaPath {
		var ok bool
		lastProperty = property
		fieldSchema, ok = fieldSchema.Properties.Get(property)
		if !ok {
			return fmt.Errorf("couldn't find schema path '%s' at '%s'", schemaPath, property)
		}

		if i+1 == len(splittedSchemaPath) {
			break
		}

		if propertyMap, ok := defaults[property]; ok {
			defaults, ok = propertyMap.(map[string]interface{})
			if !ok {
				defaults = nil
			}
		} else {
			defaults = nil
		}

		// resolve schema for next jump
		ref := ""
		if fieldSchema.Type == "array" {
			ref = fieldSchema.Items.Ref
		} else if patternPropertySchema, ok := fieldSchema.PatternProperties[".*"]; ok {
			ref = patternPropertySchema.Ref
		} else if fieldSchema.Ref != "" {
			ref = fieldSchema.Ref
		}
		if ref != "" {
			refSplit := strings.Split(ref, "/")
			fieldSchema, ok = schema.Definitions[refSplit[len(refSplit)-1]]
			if !ok {
				return fmt.Errorf("couldn't find schema definition %s", refSplit[len(refSplit)-1])
			}
		}
	}

	content := renderField(
		"",
		lastProperty,
		fieldSchema,
		schema.Definitions,
		false,
		1,
		defaults,
	)
	filePath := path.Join(basePath, schemaPath) + ".mdx"

	// write file
	_ = os.MkdirAll(path.Dir(filePath), 0o777)
	err := os.WriteFile(filePath, []byte(content), os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func GenerateResource(schema *jsonschema.Schema, basePath string, subResource bool) error {
	createSections(path.Join(basePath, "reference.mdx"), schema, schema.Definitions, subResource, false, false, 1)
	return nil
}

func createSections(pageFile string, schema *jsonschema.Schema, definitions jsonschema.Definitions, skipMetadata, subResource bool, metadataOnly bool, depth int) string {
	content := buildContent("", schema, definitions, metadataOnly, depth, nil)
	importContent := ""
	if !skipMetadata && !metadataOnly {
		if subResource {
			importContent += "\nimport Metadata from \"../../metadata/reference.mdx\""
		} else {
			importContent += "\nimport Metadata from \"../metadata/reference.mdx\""
		}
		content = "\n\n<Metadata />\n" + content
	}

	content = fmt.Sprintf("%s%s", importContent, content)
	_ = os.MkdirAll(path.Dir(pageFile), 0o777)
	err := os.WriteFile(pageFile, []byte(content), os.ModePerm)
	if err != nil {
		panic(err)
	}

	return content
}

func buildContent(prefix string, schema *jsonschema.Schema, definitions jsonschema.Definitions, metadataOnly bool, depth int, defaults interface{}) string {
	content := ""
	if schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			fieldName := pair.Key

			fieldSchema, ok := schema.Properties.Get(fieldName)
			if !ok {
				continue
			}

			if !metadataOnly {
				if fieldName == "apiVersion" && depth == 1 {
					continue
				}
				if fieldName == "kind" && depth == 1 {
					continue
				}
				if fieldName == "metadata" && depth == 1 {
					continue
				}
			} else {
				if fieldName != "apiVersion" && fieldName != "kind" && fieldName != "metadata" && depth == 1 {
					continue
				}
			}

			fieldContent := renderField(
				prefix,
				fieldName,
				fieldSchema,
				definitions,
				metadataOnly,
				depth,
				defaults,
			)
			if fieldContent != "" {
				content += "\n\n" + fieldContent
			}
		}
	}

	return content
}

func renderField(
	prefix,
	fieldName string,
	fieldSchema *jsonschema.Schema,
	definitions jsonschema.Definitions,
	metadataOnly bool,
	depth int,
	defaults interface{},
) string {
	headlinePrefix := strings.Repeat("#", int(math.Min(5, float64(depth+1)))) + " "
	anchorPrefix := strings.TrimPrefix(strings.ReplaceAll(prefix, prefixSeparator, anchorSeparator), anchorSeparator)

	if defaultsMap, ok := defaults.(map[string]interface{}); ok && defaultsMap != nil {
		defaults = defaultsMap[fieldName]
	} else {
		defaults = nil
	}

	fieldContent := ""
	isNameObjectMap := false
	isObjectOfType := false
	expandable := false

	var patternPropertySchema *jsonschema.Schema
	var ok bool

	ref := ""
	if fieldSchema.Type == "array" {
		ref = fieldSchema.Items.Ref
	} else if patternPropertySchema, ok = fieldSchema.PatternProperties[".*"]; ok {
		ref = patternPropertySchema.Ref
		isNameObjectMap = true
	} else if fieldSchema.Ref != "" {
		ref = fieldSchema.Ref
	} else if fieldSchema.AdditionalProperties != nil && fieldSchema.AdditionalProperties.Ref != "" {
		ref = fieldSchema.AdditionalProperties.Ref
		isObjectOfType = true
	}

	if ref != "" {
		refSplit := strings.Split(ref, "/")
		nestedSchema, ok := definitions[refSplit[len(refSplit)-1]]
		if ok {
			newPrefix := prefix + fieldName + prefixSeparator
			fieldContent = buildContent(newPrefix, nestedSchema, definitions, metadataOnly, depth+1, defaults)
			expandable = true
		}
	}

	required := DefaultRequire
	fieldDefault := ""

	description := fieldSchema.Description

	// replace { & }
	description = strings.ReplaceAll(description, "{", "&#123;")
	description = strings.ReplaceAll(description, "}", "&#125;")

	lines := strings.Split(description, "\n")
	newLines := []string{}
	for _, line := range lines {
		if line == "+optional" {
			required = false
		}
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			continue
		}
		newLines = append(newLines, line)
	}
	description = strings.Join(newLines, "\n")
	if fieldName == "metadata" && prefix == "" {
		required = false
	}
	if fieldName == "spec" && prefix == "" {
		required = false
	}
	if fieldName == "status" && prefix == "" {
		required = false
	}

	fieldType := fieldSchema.Type

	if fieldType == "" && fieldSchema.OneOf != nil {
		for _, oneOfType := range fieldSchema.OneOf {
			if fieldType != "" {
				fieldType = fieldType + "|"
			}
			fieldType = fieldType + oneOfType.Type
		}
	}

	if isNameObjectMap {
		fieldNameSingular := pluralizeClient.Singular(fieldName)
		fieldType = "&lt;" + fieldNameSingular + "_name&gt;:"

		if patternPropertySchema != nil && patternPropertySchema.Type != "" {
			fieldType = fieldType + patternPropertySchema.Type
		} else {
			fieldType = fieldType + "object"
		}
	}

	if fieldType == "array" {
		if fieldSchema.Items.Type == "" {
			fieldType = "object[]"
		} else {
			fieldType = fieldSchema.Items.Type + "[]"
		}
	}

	if fieldType == "" {
		fieldType = "object"
	}

	if isObjectOfType {
		fieldType = "&#123;key: " + fieldType + "&#125;"
	}

	if ref != "" {
		refSplit := strings.Split(ref, "/")
		_, ok = definitions[refSplit[len(refSplit)-1]]
		if ok {
			anchorName := anchorPrefix + fieldName
			proLabel := ""
			fieldContent = fmt.Sprintf(TemplateConfigField, true, "", headlinePrefix, fieldName, required, fieldType, "", "", proLabel, anchorName, description, fieldContent)
		}
	} else {
		if defaults != nil {
			fieldDefault = fmt.Sprintf("%v", defaults)
			if fieldDefault == "map[]" {
				fieldDefault = "{}"
			}
			fieldDefault = strings.ReplaceAll(fieldDefault, "[", "&#91;")
			fieldDefault = strings.ReplaceAll(fieldDefault, "]", "&#93;")
			fieldDefault = strings.ReplaceAll(fieldDefault, "{", "&#123;")
			fieldDefault = strings.ReplaceAll(fieldDefault, "}", "&#125;")
		}

		enumValues := GetEumValues(fieldSchema, required, &fieldDefault)
		anchorName := anchorPrefix + fieldName
		proLabel := ""
		fieldContent = fmt.Sprintf(TemplateConfigField, expandable, " open", headlinePrefix, fieldName, required, fieldType, fieldDefault, enumValues, proLabel, anchorName, description, fieldContent)
	}

	return fieldContent
}

func GetEumValues(fieldSchema *jsonschema.Schema, required bool, fieldDefault *string) string {
	enumValues := ""
	if fieldSchema.Enum != nil {
		for i, enumVal := range fieldSchema.Enum {
			enumValString, ok := enumVal.(string)
			if ok {
				if i == 0 && !required && *fieldDefault == "" {
					*fieldDefault = enumValString
				}

				if enumValues != "" {
					enumValues = enumValues + "<br/>"
				}
				enumValues = enumValues + enumValString
			}
		}
		enumValues = fmt.Sprintf("<span>%s</span>", enumValues)
	}
	return enumValues
}

func writeTemplate(templateContents, filePath string, values interface{}) {
	t, err := template.New("").Parse(templateContents)
	if err != nil {
		panic(err)
	}

	b := &bytes.Buffer{}
	err = t.Execute(b, values)
	if err != nil {
		panic(err)
	}

	_ = os.MkdirAll(path.Dir(filePath), 0o777)
	err = os.WriteFile(filePath, b.Bytes(), os.ModePerm)
	if err != nil {
		panic(err)
	}
}
