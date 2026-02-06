package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodManager struct {
	ctl *kube.Ctl
}

func NewPodManager(ctl *kube.Ctl) *PodManager {
	return &PodManager{ctl: ctl}
}

func (m *PodManager) AllPodsStable(ctx context.Context, sets []*appsv1.StatefulSet) (bool, error) {
	for _, set := range sets {
		stable, err := m.PodsStable(ctx, set)
		if err != nil {
			return false, err
		}
		if !stable {
			return false, nil
		}
	}

	return true, nil
}

func (m *PodManager) PodsStable(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
	pods, err := m.getPods(ctx, set)
	if err != nil {
		return false, err
	}

	// make sure none are in the process of terminating
	for _, pod := range pods {
		if pod.GetDeletionTimestamp() != nil {
			return false, nil
		}
	}

	desiredReplicas := ptr.Deref(set.Spec.Replicas, 1)
	actualReplicas := int32(len(pods))

	return set.Status.ObservedGeneration == set.Generation && desiredReplicas <= set.Status.ReadyReplicas && desiredReplicas <= actualReplicas, nil
}

func (m *PodManager) GetNextOutdatedPod(ctx context.Context, set *appsv1.StatefulSet) (*corev1.Pod, error) {
	revisions, err := m.GetStatefulSetRevisions(ctx, set)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, nil
	}

	pods, err := m.getPods(ctx, set)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		podRevision := pod.Labels[appsv1.StatefulSetRevisionLabel]
		if podRevision != revisions[len(revisions)-1].Name {
			return pod, nil
		}
	}

	if len(pods) > int(ptr.Deref(set.Spec.Replicas, 1)) {
		// if there are more pods than desired, we can consider the extra ones outdated
		return pods[len(pods)-1], nil
	}

	return nil, nil
}

func (m *PodManager) getPods(ctx context.Context, set *appsv1.StatefulSet) ([]*corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(set.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("constructing label selector: %w", err)
	}

	podList, err := kube.List[corev1.PodList](ctx, m.ctl, set.GetNamespace(), client.MatchingLabelsSelector{
		Selector: selector,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "listing Pods for StatefulSet %s/%s", set.GetNamespace(), set.GetName())
	}
	pods, err := kube.Items[*corev1.Pod](podList)
	if err != nil {
		return nil, err
	}

	// filter to only pods owned by this StatefulSet, since multiple
	// StatefulSets may share the same selector during migration
	owned := make([]*corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		ref := metav1.GetControllerOfNoCopy(pod)
		if ref != nil && ref.UID == set.GetUID() {
			owned = append(owned, pod)
		}
	}

	return sortPodsByOrdinal(owned...)
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

func sortPodsByOrdinal(pods ...*corev1.Pod) ([]*corev1.Pod, error) {
	ordinals := map[types.NamespacedName]int{}

	for _, pod := range pods {
		ordinal, err := extractOrdinal(pod.GetName())
		if err != nil {
			return nil, err
		}
		ordinals[client.ObjectKeyFromObject(pod)] = ordinal
	}

	sort.SliceStable(pods, func(i, j int) bool {
		return ordinals[client.ObjectKeyFromObject(pods[i])] < ordinals[client.ObjectKeyFromObject(pods[j])]
	})

	return pods, nil
}

// extractOrdinal extracts an ordinal from the pod name by parsing the last
// value after a "-" in the pod name
func extractOrdinal(name string) (int, error) {
	resourceTokens := strings.Split(name, "-")
	if len(resourceTokens) < 2 {
		return 0, fmt.Errorf("invalid resource name for ordinal fetching: %s", name)
	}

	// grab the last item after the "-"" which should be the ordinal and parse it
	ordinal, err := strconv.Atoi(resourceTokens[len(resourceTokens)-1])
	if err != nil {
		return 0, fmt.Errorf("parsing resource name %q: %w", name, err)
	}

	return ordinal, nil
}
