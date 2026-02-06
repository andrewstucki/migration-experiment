package reconcilers

import (
	"context"
	"fmt"
	"sort"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type PodManager struct {
	ctl *kube.Ctl
}

func NewPodManager(ctl *kube.Ctl) *PodManager {
	return &PodManager{ctl: ctl}
}

func (m *PodManager) GetNextOutdatedPod(ctx context.Context, set *appsv1.StatefulSet) (*corev1.Pod, error) {
	revisions, err := m.GetStatefulSetRevisions(ctx, set)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(set.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("constructing label selector: %w", err)
	}

	pods, err := kube.List[corev1.PodList](ctx, m.ctl, set.GetNamespace(), client.MatchingLabelsSelector{
		Selector: selector,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "listing Pods for StatefulSet %s/%s", set.GetNamespace(), set.GetName())
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		podRevision := pod.Labels[appsv1.StatefulSetRevisionLabel]
		if podRevision != revisions[len(revisions)-1].Name {
			return pod, nil
		}
	}

	return nil, nil
}

func (m *PodManager) GetStatefulSetRevisions(ctx context.Context, set *appsv1.StatefulSet) ([]*appsv1.ControllerRevision, error) {
	selector, err := metav1.LabelSelectorAsSelector(set.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("constructing label selector: %w", err)
	}

	// based on https://github.com/kubernetes/kubernetes/blob/c90a4b16b6aa849ed362ee40997327db09e3a62d/pkg/controller/history/controller_history.go#L222
	revisions, err := kube.List[appsv1.ControllerRevisionList](ctx, m.ctl, set.GetNamespace(), client.MatchingLabelsSelector{
		Selector: selector,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "listing ControllerRevisions")
	}

	ownedRevisions := []*appsv1.ControllerRevision{}
	for i := range revisions.Items {
		ref := metav1.GetControllerOfNoCopy(&revisions.Items[i])
		if ref == nil || ref.UID == set.GetUID() {
			ownedRevisions = append(ownedRevisions, &revisions.Items[i])
		}
	}
	return sortRevisions(ownedRevisions), nil
}

// sortRevisions sorts a statefulset's controlerRevisions by revision number
func sortRevisions(controllerRevisions []*appsv1.ControllerRevision) []*appsv1.ControllerRevision {
	// from https://github.com/kubernetes/kubernetes/blob/dd25c6a6cb4ea0be1e304de35de45adeef78b264/pkg/controller/history/controller_history.go#L158
	sort.SliceStable(controllerRevisions, func(i, j int) bool {
		if controllerRevisions[i].Revision == controllerRevisions[j].Revision {
			if controllerRevisions[j].CreationTimestamp.Equal(&controllerRevisions[i].CreationTimestamp) {
				return controllerRevisions[i].Name < controllerRevisions[j].Name
			}
			return controllerRevisions[j].CreationTimestamp.After(controllerRevisions[i].CreationTimestamp.Time)
		}
		return controllerRevisions[i].Revision < controllerRevisions[j].Revision
	})

	return controllerRevisions
}
