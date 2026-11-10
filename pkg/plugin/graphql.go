package plugin

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

func validateGraphQL(source, operationName string) error {
	document, err := parser.ParseQueryWithTokenLimit(&ast.Source{Input: source}, 100000)
	if err != nil {
		return fmt.Errorf("invalid GraphQL: %w", err)
	}
	if len(document.Operations) == 0 {
		return fmt.Errorf("enter a GraphQL query operation")
	}
	for _, operation := range document.Operations {
		if operation.Operation != ast.Query {
			return fmt.Errorf("only GraphQL query operations are supported")
		}
	}
	if len(document.Operations) > 1 && operationName == "" {
		return fmt.Errorf("set an operation name when the GraphQL document contains multiple queries")
	}
	if operationName != "" && document.Operations.ForName(operationName) == nil {
		return fmt.Errorf("GraphQL operation %q was not found", operationName)
	}
	return nil
}
