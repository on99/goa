package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"github.com/goadesign/goa/design"
	"github.com/goadesign/goa/dslengine"
)

var (
	// TempCount holds the value appended to variable names to make them unique.
	TempCount int
)

// GoTypeDef returns the Go code that defines a Go type which matches the data structure
// definition (the part that comes after `type foo`).
// versioned indicates whether the type is being referenced from a version package (true) or the
// default package (false).
// tabs is the number of tab character(s) used to tabulate the definition however the first
// line is never indented.
// jsonTags controls whether to produce json tags.
func GoTypeDef(ds design.DataStructure, versioned bool, defPkg string, tabs int, jsonTags bool) string {
	var buffer bytes.Buffer
	def := ds.Definition()
	t := def.Type
	switch actual := t.(type) {
	case design.Primitive:
		return GoTypeName(t, nil, tabs)
	case *design.Array:
		d := GoTypeDef(actual.ElemType, versioned, defPkg, tabs, jsonTags)
		if actual.ElemType.Type.IsObject() {
			d = "*" + d
		}
		return "[]" + d
	case *design.Hash:
		keyDef := GoTypeDef(actual.KeyType, versioned, defPkg, tabs, jsonTags)
		if actual.KeyType.Type.IsObject() {
			keyDef = "*" + keyDef
		}
		elemDef := GoTypeDef(actual.ElemType, versioned, defPkg, tabs, jsonTags)
		if actual.ElemType.Type.IsObject() {
			elemDef = "*" + elemDef
		}
		return fmt.Sprintf("map[%s]%s", keyDef, elemDef)
	case design.Object:
		buffer.WriteString("struct {\n")
		keys := make([]string, len(actual))
		i := 0
		for n := range actual {
			keys[i] = n
			i++
		}
		sort.Strings(keys)
		for _, name := range keys {
			WriteTabs(&buffer, tabs+1)
			field := actual[name]
			typedef := GoTypeDef(field, versioned, defPkg, tabs+1, jsonTags)
			if field.Type.IsObject() || def.IsPrimitivePointer(name) {
				typedef = "*" + typedef
			}
			fname := Goify(name, true)
			var tags string
			if jsonTags {
				var omit string
				if !def.IsRequired(name) {
					omit = ",omitempty"
				}
				tags = fmt.Sprintf(" `json:\"%s%s\" xml:\"%s%s\"`", name, omit, name, omit)
			}
			desc := actual[name].Description
			if desc != "" {
				desc = fmt.Sprintf("// %s\n", desc)
			}
			buffer.WriteString(fmt.Sprintf("%s%s %s%s\n", desc, fname, typedef, tags))
		}
		WriteTabs(&buffer, tabs)
		buffer.WriteString("}")
		return buffer.String()
	case *design.UserTypeDefinition:
		return GoPackageTypeName(actual, actual.AllRequired(), versioned, defPkg, tabs)
	case *design.MediaTypeDefinition:
		return GoPackageTypeName(actual, actual.AllRequired(), versioned, defPkg, tabs)
	default:
		panic("goa bug: unknown data structure type")
	}
}

// GoTypeRef returns the Go code that refers to the Go type which matches the given data type
// (the part that comes after `var foo`)
// required only applies when referring to a user type that is an object defined inline. In this
// case the type (Object) does not carry the required field information defined in the parent
// (anonymous) attribute.
// tabs is used to properly tabulate the object struct fields and only applies to this case.
// This function assumes the type is in the same package as the code accessing it.
func GoTypeRef(t design.DataType, required []string, tabs int) string {
	return GoPackageTypeRef(t, required, false, "", tabs)
}

// GoPackageTypeRef returns the Go code that refers to the Go type which matches the given data type.
// versioned indicates whether the type is being referenced from a version package (true) or the
// default package defPkg (false).
// required only applies when referring to a user type that is an object defined inline. In this
// case the type (Object) does not carry the required field information defined in the parent
// (anonymous) attribute.
// tabs is used to properly tabulate the object struct fields and only applies to this case.
func GoPackageTypeRef(t design.DataType, required []string, versioned bool, defPkg string, tabs int) string {
	switch t.(type) {
	case *design.UserTypeDefinition, *design.MediaTypeDefinition:
		var prefix string
		if t.IsObject() {
			prefix = "*"
		}
		return prefix + GoPackageTypeName(t, required, versioned, defPkg, tabs)
	case design.Object:
		return "*" + GoPackageTypeName(t, required, versioned, defPkg, tabs)
	default:
		return GoPackageTypeName(t, required, versioned, defPkg, tabs)
	}
}

// GoTypeName returns the Go type name for a data type.
// tabs is used to properly tabulate the object struct fields and only applies to this case.
// This function assumes the type is in the same package as the code accessing it.
// required only applies when referring to a user type that is an object defined inline. In this
// case the type (Object) does not carry the required field information defined in the parent
// (anonymous) attribute.
func GoTypeName(t design.DataType, required []string, tabs int) string {
	return GoPackageTypeName(t, required, false, "", tabs)
}

// GoPackageTypeName returns the Go type name for a data type.
// versioned indicates whether the type is being referenced from a version package (true) or the
// default package defPkg (false).
// required only applies when referring to a user type that is an object defined inline. In this
// case the type (Object) does not carry the required field information defined in the parent
// (anonymous) attribute.
// tabs is used to properly tabulate the object struct fields and only applies to this case.
func GoPackageTypeName(t design.DataType, required []string, versioned bool, defPkg string, tabs int) string {
	switch actual := t.(type) {
	case design.Primitive:
		return GoNativeType(t)
	case *design.Array:
		return "[]" + GoPackageTypeRef(actual.ElemType.Type, actual.ElemType.AllRequired(), versioned, defPkg, tabs+1)
	case design.Object:
		att := &design.AttributeDefinition{Type: actual}
		if len(required) > 0 {
			requiredVal := &dslengine.RequiredValidationDefinition{Names: required}
			att.Validations = append(att.Validations, requiredVal)
		}
		return GoTypeDef(att, versioned, defPkg, tabs, false)
	case *design.Hash:
		return fmt.Sprintf(
			"map[%s]%s",
			GoPackageTypeRef(actual.KeyType.Type, actual.KeyType.AllRequired(), versioned, defPkg, tabs+1),
			GoPackageTypeRef(actual.ElemType.Type, actual.ElemType.AllRequired(), versioned, defPkg, tabs+1),
		)
	case *design.UserTypeDefinition:
		pkgPrefix := PackagePrefix(actual, versioned, defPkg)
		return pkgPrefix + Goify(actual.TypeName, true)
	case *design.MediaTypeDefinition:
		pkgPrefix := PackagePrefix(actual.UserTypeDefinition, versioned, defPkg)
		return pkgPrefix + Goify(actual.TypeName, true)
	default:
		panic(fmt.Sprintf("goa bug: unknown type %#v", actual))
	}
}

// GoNativeType returns the Go built-in type from which instances of t can be initialized.
func GoNativeType(t design.DataType) string {
	switch actual := t.(type) {
	case design.Primitive:
		switch actual.Kind() {
		case design.BooleanKind:
			return "bool"
		case design.IntegerKind:
			return "int"
		case design.NumberKind:
			return "float64"
		case design.StringKind:
			return "string"
		case design.DateTimeKind:
			return "time.Time"
		case design.AnyKind:
			return "interface{}"
		default:
			panic(fmt.Sprintf("goa bug: unknown primitive type %#v", actual))
		}
	case *design.Array:
		return "[]" + GoNativeType(actual.ElemType.Type)
	case design.Object:
		return "map[string]interface{}"
	case *design.Hash:
		return fmt.Sprintf("map[%s]%s", GoNativeType(actual.KeyType.Type), GoNativeType(actual.ElemType.Type))
	case *design.MediaTypeDefinition:
		return GoNativeType(actual.Type)
	case *design.UserTypeDefinition:
		return GoNativeType(actual.Type)
	default:
		panic(fmt.Sprintf("goa bug: unknown type %#v", actual))
	}
}

// Goify makes a valid Go identifier out of any string.
// It does that by removing any non letter and non digit character and by making sure the first
// character is a letter or "_".
// Goify produces a "CamelCase" version of the string, if firstUpper is true the first character
// of the identifier is uppercase otherwise it's lowercase.
func Goify(str string, firstUpper bool) string {
	if str == "ok" && firstUpper {
		return "OK"
	} else if str == "id" && firstUpper {
		return "ID"
	}
	var b bytes.Buffer
	var firstWritten, nextUpper bool
	for i := 0; i < len(str); i++ {
		r := rune(str[i])
		if r == '_' {
			nextUpper = true
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !firstWritten {
				if firstUpper {
					r = unicode.ToUpper(r)
				} else {
					r = unicode.ToLower(r)
				}
				firstWritten = true
				nextUpper = false
			} else if nextUpper {
				r = unicode.ToUpper(r)
				nextUpper = false
			}
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "_v" // you have a better idea?
	}
	res := b.String()
	if _, ok := reserved[res]; ok {
		res += "_"
	}
	return res
}

// WriteTabs is a helper function that writes count tabulation characters to buf.
func WriteTabs(buf *bytes.Buffer, count int) {
	for i := 0; i < count; i++ {
		buf.WriteByte('\t')
	}
}

// Tempvar generates a unique variable name.
func Tempvar() string {
	TempCount++
	return fmt.Sprintf("tmp%d", TempCount)
}

// PackagePrefix returns the package prefix to use to access ut from ver given it lives in the
// package pkg.
func PackagePrefix(ut *design.UserTypeDefinition, versioned bool, pkg string) string {
	if !versioned {
		// If the version is the default version then the user type is in the same package
		// (otherwise the DSL would not be valid).
		return ""
	}
	if len(ut.APIVersions) == 0 {
		// If the type is not versioned but we are accessing it from the non-default version
		// then we need to qualify it with the default version package.
		return pkg + "."
	}
	// If the type is versioned then we must be accessing it from the current version
	// (unversioned definitions cannot use versioned definitions)
	return ""
}

// RunTemplate executs the given template with the given input and returns
// the rendered string.
func RunTemplate(tmpl *template.Template, data interface{}) string {
	var b bytes.Buffer
	err := tmpl.Execute(&b, data)
	if err != nil {
		panic(err) // should never happen, bug if it does.
	}
	return b.String()
}

// reserved golang keywords
var reserved = map[string]bool{
	"byte":       true,
	"complex128": true,
	"complex64":  true,
	"float32":    true,
	"float64":    true,
	"int":        true,
	"int16":      true,
	"int32":      true,
	"int64":      true,
	"int8":       true,
	"rune":       true,
	"string":     true,
	"uint16":     true,
	"uint32":     true,
	"uint64":     true,
	"uint8":      true,

	"break":       true,
	"case":        true,
	"chan":        true,
	"const":       true,
	"continue":    true,
	"default":     true,
	"defer":       true,
	"else":        true,
	"fallthrough": true,
	"for":         true,
	"func":        true,
	"go":          true,
	"goto":        true,
	"if":          true,
	"import":      true,
	"interface":   true,
	"map":         true,
	"package":     true,
	"range":       true,
	"return":      true,
	"select":      true,
	"struct":      true,
	"switch":      true,
	"type":        true,
	"var":         true,
}

// has returns true is slice contains val, false otherwise.
func has(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// toJSON returns the JSON representation of the given value.
func toJSON(val interface{}) string {
	js, err := json.Marshal(val)
	if err != nil {
		return "<error serializing value>"
	}
	return string(js)
}

// toSlice returns Go code that represents the given slice.
func toSlice(val []interface{}) string {
	elems := make([]string, len(val))
	for i, v := range val {
		elems[i] = fmt.Sprintf("%#v", v)
	}
	return fmt.Sprintf("[]interface{}{%s}", strings.Join(elems, ", "))
}
