package render

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	"github.com/redpanda-data/common-go/kube"
)

type PObject[T any] interface {
	client.Object
	*T
}

type Syncer interface {
	DeleteAll(ctx context.Context) (bool, error)
	Sync(ctx context.Context) ([]client.Object, error)
}

type SyncerFactory[T any, PT PObject[T]] interface {
	Syncer(state PT) Syncer
}

func NewSyncerFactoryForKubeCtl(ctl *kube.Ctl) SyncerFactory[migrationv1alpha1.New, *migrationv1alpha1.New] {
	return &kubeCtlSyncerFactory[migrationv1alpha1.New, *migrationv1alpha1.New]{
		ctl:       ctl,
		renderer:  RenderNew,
		types:     NewRenderedTypes,
		ownership: NewOwnershipLabels,
	}
}

func OldSyncerFactoryForKubeCtl(ctl *kube.Ctl) SyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old] {
	return &kubeCtlSyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old]{
		ctl:       ctl,
		renderer:  RenderOld,
		types:     OldRenderedTypes,
		ownership: OldOwnershipLabels,
	}
}

type kubeCtlSyncerFactory[T any, PT PObject[T]] struct {
	renderer  func(context.Context, PT) ([]kube.Object, error)
	types     func() []kube.Object
	ownership func(o PT) map[string]string
	ctl       *kube.Ctl
}

func (f *kubeCtlSyncerFactory[T, PT]) Syncer(state PT) Syncer {
	return &kube.Syncer{
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
	}
}

type renderer[T any, PT PObject[T]] struct {
	state    PT
	renderer func(context.Context, PT) ([]kube.Object, error)
	types    func() []kube.Object
}

func newRenderer[T any, PT PObject[T]](
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
