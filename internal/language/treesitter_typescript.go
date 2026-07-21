package language

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func isTypeScriptFamily(language Language) bool {
	switch language {
	case TypeScript, TSX, JavaScript, JSX:
		return true
	default:
		return false
	}
}

// treeSitterTypeScriptDefinitions preserves the named-function shapes ccr
// reviewed before the gotreesitter migration. Generic tags cover declarations
// and class methods; this walker additionally gives names to arrows/function
// expressions attached to variables, fields, object properties, and namespaces.
func treeSitterTypeScriptDefinitions(source Source, tree *gotreesitter.Tree) []treeSitterDefinition {
	lang := tree.Language()
	var definitions []treeSitterDefinition
	var walk func(*gotreesitter.Node, string)
	walk = func(node *gotreesitter.Node, owner string) {
		if node == nil {
			return
		}
		typ := node.Type(lang)
		switch typ {
		case "internal_module", "ambient_declaration":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" {
				owner = joinTypeScriptOwner(owner, name)
			}
		case "class_declaration", "class":
			name := treeSitterNodeName(node, lang, source.Content)
			if name == "" {
				return
			}
			fullName := joinTypeScriptOwner(owner, name)
			definitions = append(definitions, newTypeScriptDefinition(source, fullName, owner, KindClass, node))
			owner = fullName
		case "interface_declaration":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" {
				definitions = append(definitions, newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, KindInterface, node))
			}
			return
		case "type_alias_declaration", "enum_declaration":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" {
				definitions = append(definitions, newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, KindType, node))
			}
			return
		case "function_declaration", "generator_function_declaration":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" {
				definitions = append(definitions, newTypeScriptCallable(source, name, owner, node))
			}
			return
		case "method_definition", "method_signature":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" {
				definitions = append(definitions, newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, KindMethod, node))
			}
			return
		case "public_field_definition":
			name := treeSitterNodeName(node, lang, source.Content)
			if name != "" && treeSitterFunctionLike(treeSitterNodeValue(node, lang), lang) != nil {
				definitions = append(definitions, newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, KindMethod, node))
			}
			return
		case "variable_declarator":
			name := treeSitterNodeName(node, lang, source.Content)
			value := treeSitterNodeValue(node, lang)
			if name == "" || value == nil {
				return
			}
			fullName := joinTypeScriptOwner(owner, name)
			if treeSitterFunctionLike(value, lang) != nil {
				definitions = append(definitions, newTypeScriptCallable(source, name, owner, node))
				return
			}
			if value.Type(lang) == "object" {
				collectTypeScriptObject(source, value, fullName, lang, &definitions)
			}
			return
		}
		for i := 0; i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i), owner)
		}
	}
	walk(tree.RootNode(), "")
	return dedupeTreeSitterDefinitions(definitions)
}

func collectTypeScriptObject(source Source, object *gotreesitter.Node, owner string, lang *gotreesitter.Language, definitions *[]treeSitterDefinition) {
	for i := 0; i < object.NamedChildCount(); i++ {
		property := object.NamedChild(i)
		if property == nil {
			continue
		}
		switch property.Type(lang) {
		case "method_definition":
			name := treeSitterNodeName(property, lang, source.Content)
			if name != "" {
				*definitions = append(*definitions, newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, KindMethod, property))
			}
		case "pair":
			name := treeSitterNodeName(property, lang, source.Content)
			value := treeSitterNodeValue(property, lang)
			if name == "" || value == nil {
				continue
			}
			fullName := joinTypeScriptOwner(owner, name)
			if treeSitterFunctionLike(value, lang) != nil {
				*definitions = append(*definitions, newTypeScriptDefinition(source, fullName, owner, KindMethod, property))
			} else if value.Type(lang) == "object" {
				collectTypeScriptObject(source, value, fullName, lang, definitions)
			}
		}
	}
}

func newTypeScriptCallable(source Source, name, owner string, node *gotreesitter.Node) treeSitterDefinition {
	kind := KindFunction
	if owner != "" {
		kind = KindMethod
	}
	return newTypeScriptDefinition(source, joinTypeScriptOwner(owner, name), owner, kind, node)
}

func newTypeScriptDefinition(source Source, name, owner string, kind Kind, node *gotreesitter.Node) treeSitterDefinition {
	definition := newTreeSitterDefinition(source, name, kind, node.StartByte(), node.EndByte())
	definition.Owner = owner
	return definition
}

func treeSitterNodeName(node *gotreesitter.Node, lang *gotreesitter.Language, content string) string {
	for _, field := range []string{"name", "key"} {
		if child := node.ChildByFieldName(field, lang); child != nil {
			return strings.TrimSpace(child.Text([]byte(content)))
		}
	}
	for i := 0; i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Type(lang) {
		case "identifier", "property_identifier", "private_property_identifier", "type_identifier", "string", "number":
			return strings.Trim(strings.TrimSpace(child.Text([]byte(content))), "\"'")
		}
	}
	return ""
}

func treeSitterNodeValue(node *gotreesitter.Node, lang *gotreesitter.Language) *gotreesitter.Node {
	for _, field := range []string{"value", "initializer"} {
		if child := node.ChildByFieldName(field, lang); child != nil {
			return child
		}
	}
	if count := node.NamedChildCount(); count > 1 {
		return node.NamedChild(count - 1)
	}
	return nil
}

func treeSitterFunctionLike(node *gotreesitter.Node, lang *gotreesitter.Language) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	switch node.Type(lang) {
	case "arrow_function", "function_expression", "generator_function":
		return node
	case "parenthesized_expression", "as_expression", "type_assertion", "satisfies_expression", "non_null_expression", "call_expression":
		for i := 0; i < node.NamedChildCount(); i++ {
			if function := treeSitterFunctionLike(node.NamedChild(i), lang); function != nil {
				return function
			}
		}
	}
	return nil
}

func treeSitterTypeScriptReferences(tree *gotreesitter.Tree) map[string]int {
	lang := tree.Language()
	references := map[string]int{}
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		switch node.Type(lang) {
		case "identifier", "property_identifier", "private_property_identifier", "type_identifier":
			name := strings.TrimPrefix(strings.TrimSpace(node.Text(tree.Source())), "#")
			if len(name) >= 3 {
				references[name]++
			}
		}
		for i := 0; i < node.NamedChildCount(); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	return references
}

func joinTypeScriptOwner(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

func dedupeTreeSitterDefinitions(definitions []treeSitterDefinition) []treeSitterDefinition {
	type key struct {
		symbolID string
		start    uint32
		end      uint32
	}
	seen := map[key]bool{}
	out := make([]treeSitterDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definitionKey := key{symbolID: definition.SymbolID, start: definition.startByte, end: definition.endByte}
		if seen[definitionKey] {
			continue
		}
		seen[definitionKey] = true
		out = append(out, definition)
	}
	return out
}
