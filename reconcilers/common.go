package reconcilers

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const Finalizer = "migration.lambda.coffee/finalizer"

func maybeWatchResources(scheme *runtime.Scheme, mapper meta.RESTMapper, b *builder.Builder, resources []kube.Object, nameLabel, namespaceLabel string) {
	for _, o := range resources {
		gvk, err := apiutil.GVKForObject(o, scheme)
		if err != nil {
			continue
		}

		_, err = mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			continue
		}

		b.Watches(o, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			if obj.GetLabels() == nil {
				return nil
			}
			name, ok := obj.GetLabels()[nameLabel]
			if !ok {
				return nil
			}
			namespace, ok := obj.GetLabels()[namespaceLabel]
			if !ok {
				return nil
			}
			return []ctrl.Request{{
				NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
			}}
		}))
	}
}
