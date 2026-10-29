package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/didit-protocol/sdk-go/internal/naming"
)

// compatAlias is a type alias of openapi/compatibility.json, checked against
// the generated declarations.
type compatAlias struct {
	Name   string
	Target *Decl
	// Values are the released constants of an enum alias.
	Values []EnumValue
}

// compatAliases returns the type aliases of openapi/compatibility.json by the
// name of the type they alias. It fails when an entry no longer applies: its
// name is generated again, or its target or one of its constants is not.
func (g *Gen) compatAliases() (map[string][]compatAlias, error) {
	names := map[string]bool{}
	for _, d := range g.decls {
		if len(d.Owners) == 0 {
			continue
		}
		names[d.Name] = true
		for _, v := range d.Values {
			names[v.GoName] = true
		}
	}
	out := map[string][]compatAlias{}
	var problems []string
	for _, name := range sortedKeys(g.spec.Compat.TypeAliases) {
		entry := g.spec.Compat.TypeAliases[name]
		target := g.byName[entry.AliasOf]
		switch {
		case names[name] || g.reserved[name] != "":
			problems = append(problems, fmt.Sprintf("%s is generated again; delete the entry", name))
			continue
		case target == nil || len(target.Owners) == 0:
			problems = append(problems, fmt.Sprintf("%s: alias_of %s is not a generated type", name, entry.AliasOf))
			continue
		case len(entry.Values) > 0 && target.Kind != DEnum:
			problems = append(problems, fmt.Sprintf("%s: values are given but %s is not an enum", name, entry.AliasOf))
			continue
		}
		names[name] = true
		a := compatAlias{Name: name, Target: target}
		used := map[string]bool{}
		for _, v := range entry.Values {
			// The constant names enumType gives the same values.
			suffix := naming.Pascal(v)
			if suffix == "" {
				suffix = "Empty"
			}
			c := uniqueName(name+suffix, used)
			if names[c] {
				problems = append(problems, fmt.Sprintf("%s: constant %s is generated again; drop %q from values", name, c, v))
				continue
			}
			names[c] = true
			a.Values = append(a.Values, EnumValue{GoName: c, Value: v})
		}
		out[target.Name] = append(out[target.Name], a)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("openapi/compatibility.json type_aliases:\n  %s", strings.Join(problems, "\n  "))
	}
	return out, nil
}

// emitAlias renders a released type name as a deprecated alias, followed by
// the released constants of an enum.
func emitAlias(b *bytes.Buffer, a compatAlias) {
	writeDoc(b, "", fmt.Sprintf("%s is the name of an earlier release for what the API now describes as %s, kept so that code written against that release still compiles.\n\nDeprecated: Use %s.", a.Name, a.Target.Name, a.Target.Name))
	fmt.Fprintf(b, "type %s = %s\n\n", a.Name, a.Target.Name)
	if len(a.Values) == 0 {
		return
	}
	fmt.Fprintf(b, "// Known values of %s, kept from an earlier release.\nconst (\n", a.Name)
	for _, v := range a.Values {
		replacement := "a value of " + a.Target.Name
		for _, tv := range a.Target.Values {
			if tv.Value == v.Value {
				replacement = tv.GoName
			}
		}
		fmt.Fprintf(b, "\t// Deprecated: Use %s.\n\t%s %s = %s\n", replacement, v.GoName, a.Name, strconv.Quote(v.Value))
	}
	b.WriteString(")\n\n")
}
