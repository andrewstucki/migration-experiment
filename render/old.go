package render

import (
	"context"

	"github.com/redpanda-data/common-go/kube"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
)

func RenderOld(ctx context.Context, state *migrationv1alpha1.Old) ([]kube.Object, error) {
	return []kube.Object{}, nil
}

func OldRenderedTypes() []kube.Object {
	return []kube.Object{}
}

func OldOwnershipLabels(state *migrationv1alpha1.Old) map[string]string {
	return map[string]string{
		OldNameLabelKey:      state.GetName(),
		OldNamespaceLabelKey: state.GetNamespace(),
	}
}
