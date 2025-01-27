package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var relevantPaths = []string{
	"/v1/org/{org_id}/blob/sign/{key_id}",
	"/v1/org/{org_id}/token/refresh",
	"/v0/org/{org_id}/keys/{key_id}",
}

func getComponentKey(ref string) (string, string) {
	parts := strings.Split(ref, "/")
	componentName := parts[len(parts)-1]
	componentType := parts[len(parts)-2]
	return componentType, componentName
}

func getComponent(components map[string]map[string]interface{}, componentType, componentName string) interface{} {
	return components[componentType][componentName]
}

func getRefs(obj interface{}) []string {
	refs := []string{}

	switch v := obj.(type) {
	case map[string]interface{}:
		if ref, ok := v["$ref"].(string); ok {
			refs = append(refs, ref)
		}
		for _, val := range v {
			refs = append(refs, getRefs(val)...)
		}
	case []interface{}:
		for _, item := range v {
			refs = append(refs, getRefs(item)...)
		}
	}

	return refs
}

func main() {
	// Read OpenAPI JSON
	data, err := os.ReadFile("openapi.json")
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	var api map[string]interface{}
	if err := json.Unmarshal(data, &api); err != nil {
		fmt.Println("Error parsing JSON:", err)
		return
	}

	components := api["components"].(map[string]interface{})
	typedComponents := make(map[string]map[string]interface{})
	for key, value := range components {
		typedComponents[key] = value.(map[string]interface{})
	}
	newComponents := map[string]interface{}{
		"schemas":   map[string]interface{}{},
		"responses": map[string]interface{}{},
	}

	// Copy existing components
	for k, v := range components {
		newComponents[k] = v
	}

	refs := []string{}
	paths := api["paths"].(map[string]interface{})

	// Get initial refs from relevant paths
	for _, path := range relevantPaths {
		refs = append(refs, getRefs(paths[path])...)
	}

	// Resolve and collect referenced components
	for len(refs) > 0 {
		currentRefs := refs
		refs = []string{}

		for _, ref := range currentRefs {
			componentType, componentName := getComponentKey(ref)

			// Ensure component type exists in newComponents
			if _, ok := newComponents[componentType]; !ok {
				newComponents[componentType] = map[string]interface{}{}
			}
			// Add component if not already present
			if _, ok := newComponents[componentType].(map[string]interface{})[componentName]; !ok {
				newComponents[componentType].(map[string]interface{})[componentName] =
					getComponent(typedComponents, componentType, componentName)
			}
		}

		// Get new refs from schemas and responses
		if schemas, ok := newComponents["schemas"].(map[string]interface{}); ok {
			refs = append(refs, getRefs(schemas)...)
		}
		if responses, ok := newComponents["responses"].(map[string]interface{}); ok {
			refs = append(refs, getRefs(responses)...)
		}

		// Filter out already processed refs
		filteredRefs := []string{}
		for _, ref := range refs {
			componentType, componentName := getComponentKey(ref)
			if _, ok := newComponents[componentType].(map[string]interface{})[componentName]; !ok {
				filteredRefs = append(filteredRefs, ref)
			}
		}
		refs = filteredRefs
	}

	// Update API spec
	api["components"] = newComponents

	// Filter paths
	filteredPaths := map[string]interface{}{}
	for _, path := range relevantPaths {
		filteredPaths[path] = paths[path]
	}
	api["paths"] = filteredPaths

	// Write filtered OpenAPI spec
	output, err := json.MarshalIndent(api, "", "  ")
	if err != nil {
		fmt.Println("Error marshaling JSON:", err)
		return
	}

	if err := os.WriteFile("filtered-openapi.json", output, 0644); err != nil {
		fmt.Println("Error writing file:", err)
	}
}
