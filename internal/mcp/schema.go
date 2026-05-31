// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// inputSchema infers the JSON Schema for In from its struct tags and then stamps an
// "enum" constraint onto the named properties. The schema-inference library only reads
// the jsonschema tag as a property description — it has no tag syntax for enums — so a
// closed set of allowed string values (visibility, edit mode, permission) can only be
// advertised by post-processing the inferred schema. Setting it makes the constraint part
// of the published tool schema, so a client can reject an out-of-set value before the call
// rather than relying solely on the handler's runtime check.
//
// It panics if inference fails or a named property is absent: In is a fixed local type and
// the property names are compile-time constants, so either is a programmer error that must
// surface at startup, not a per-request condition.
func inputSchema[In any](enums map[string][]any) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("infer schema for %T: %v", *new(In), err))
	}
	for prop, vals := range enums {
		ps, ok := s.Properties[prop]
		if !ok {
			panic(fmt.Sprintf("enum on unknown property %q of %T", prop, *new(In)))
		}
		ps.Enum = vals
	}
	return s
}
