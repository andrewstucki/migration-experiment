package render

import (
	"context"

	"github.com/redpanda-data/common-go/kube"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
)

func RenderNew(ctx context.Context, state *migrationv1alpha1.New) ([]kube.Object, error) {
	return []kube.Object{}, nil
}

func NewRenderedTypes() []kube.Object {
	return []kube.Object{}
}

func NewOwnershipLabels(state *migrationv1alpha1.New) map[string]string {
	return map[string]string{
		NewNameLabelKey:      state.GetName(),
		NewNamespaceLabelKey: state.GetNamespace(),
	}
}
