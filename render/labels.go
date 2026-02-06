package render

import "maps"

const (
	NewNameLabelKey      = "migration.lambda.coffee/new-name"
	OldNameLabelKey      = "migration.lambda.coffee/old-name"
	NewNamespaceLabelKey = "migration.lambda.coffee/new-namespace"
	OldNamespaceLabelKey = "migration.lambda.coffee/old-namespace"
)

func statefulsetSelectorLabels(name string) map[string]string {
	return map[string]string{
		"migration.lambda.coffee/statefulset": name,
	}
}

func mergeLabels(a, b map[string]string) map[string]string {
	maps.Copy(a, b)
	return a
}
