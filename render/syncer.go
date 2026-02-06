package render

import (
	"context"
	"slices"

	"github.com/redpanda-data/common-go/kube"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	migrationkube "github.com/andrewstucki/migration-experiment/kube"
)

func NewSyncerFactoryForKubeCtl(ctl *kube.Ctl) migrationkube.SyncerFactory[migrationv1alpha1.New, *migrationv1alpha1.New] {
	return &kubeCtlSyncerFactory[migrationv1alpha1.New, *migrationv1alpha1.New]{
		ctl:       ctl,
		renderer:  RenderNew,
		types:     NewRenderedTypes,
		ownership: NewOwnershipLabels,
	}
}

func OldSyncerFactoryForKubeCtl(ctl *kube.Ctl) migrationkube.SyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old] {
	return &kubeCtlSyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old]{
		ctl:       ctl,
		renderer:  RenderOld,
		types:     OldRenderedTypes,
		ownership: OldOwnershipLabels,
	}
}

type kubeCtlSyncerFactory[T any, PT migrationkube.PObject[T]] struct {
	renderer  func(context.Context, PT) ([]kube.Object, error)
	types     func() []kube.Object
	ownership func(o PT) map[string]string
	ctl       *kube.Ctl
}

func (f *kubeCtlSyncerFactory[T, PT]) Syncer(state PT) migrationkube.Syncer {
	return &wrappedSyncer[T, PT]{
		Syncer: &kube.Syncer{
			Ctl:       f.ctl,
			Renderer:  newRenderer(f.renderer, f.types, state),
			Namespace: state.GetNamespace(),
			Owner: metav1.OwnerReference{
				APIVersion:         "migration.lambda.coffee/v1alpha1",
				Kind:               state.GetObjectKind().GroupVersionKind().Kind,
				Name:               state.GetName(),
				UID:                state.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
			OwnershipLabels: f.ownership(state),
		},
		obj: state,
	}
}

type wrappedSyncer[T any, PT migrationkube.PObject[T]] struct {
	*kube.Syncer
	obj PT
}

func (s *wrappedSyncer[T, PT]) OwnerLabels() map[string]string {
	return s.OwnershipLabels
}

func (s *wrappedSyncer[T, PT]) ListInPurview(ctx context.Context) ([]client.Object, error) {
	logger := kube.Logger(ctx)

	var objects []client.Object
	for _, t := range s.Renderer.Types() {
		gvk, err := kube.GVKFor(s.Ctl.Scheme(), t)
		if err != nil {
			return nil, err
		}

		scope, err := s.Ctl.ScopeOf(gvk)
		if err != nil {
			// If we encounter an unknown type, e.g. someone hasn't installed
			// cert-manager, don't block the entire sync process. Instead we'll
			// log a warning and move on.
			if meta.IsNoMatchError(err) {
				// the WARNING messages here get logged constantly and are fairly static containing the resource type itself
				// so we can just use the global debouncer which debounces by error string
				kube.DebounceError(logger, err, "WARNING no registered value for resource type", "gvk", gvk.String())
				continue
			}
			return nil, err
		}

		list, err := kube.ListFor(s.Ctl.Scheme(), t)
		if err != nil {
			return nil, err
		}

		if err := s.Ctl.List(ctx, s.Namespace, list, client.MatchingLabels(s.OwnershipLabels)); err != nil {
			return nil, err
		}

		items, err := kube.Items[client.Object](list)
		if err != nil {
			return nil, err
		}

		// If resources are Namespace scoped, we additionally filter on whether
		// or not OwnerRef is set correctly.
		if scope == meta.RESTScopeNameNamespace {
			i := 0
			for _, obj := range items {
				owned := slices.ContainsFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
					return ref.UID == s.Owner.UID
				})

				if owned {
					items[i] = obj
					i++
				}
			}

			items = items[:i]
		}

		objects = append(objects, items...)
	}

	return objects, nil
}

type renderer[T any, PT migrationkube.PObject[T]] struct {
	state    PT
	renderer func(context.Context, PT) ([]kube.Object, error)
	types    func() []kube.Object
}

func newRenderer[T any, PT migrationkube.PObject[T]](
	renderFn func(context.Context, PT) ([]kube.Object, error),
	typesFn func() []kube.Object,
	state PT,
) renderer[T, PT] {
	return renderer[T, PT]{
		renderer: renderFn,
		types:    typesFn,
		state:    state,
	}
}

func (r renderer[T, PT]) Render(ctx context.Context) ([]kube.Object, error) {
	return r.renderer(ctx, r.state)
}

func (r renderer[T, PT]) Types() []kube.Object {
	return r.types()
}
