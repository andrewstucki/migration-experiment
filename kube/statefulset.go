package kube

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulSetScaler struct {
	ctl *kube.Ctl
}

func NewStatefulSetScaler(ctl *kube.Ctl) *StatefulSetScaler {
	return &StatefulSetScaler{ctl: ctl}
}

func (s *StatefulSetScaler) ScaleDownFirstUndesired(ctx context.Context, existing, desired []*appsv1.StatefulSet) (*appsv1.StatefulSet, error) {
	for _, set := range existing {
		if !containsStatefulSet(desired, set) {
			deleted, err := s.scaleDownStatefulSet(ctx, set)
			if err != nil {
				return nil, err
			}
			if !deleted {
				return set, nil
			}
		}
	}

	return nil, nil
}

func (s *StatefulSetScaler) scaleDownStatefulSet(ctx context.Context, set *appsv1.StatefulSet) (bool, error) {
	// if the set is already scaled down, delete it immediately
	if ptr.Deref(set.Spec.Replicas, 1) == 0 {
		return true, s.ctl.Delete(ctx, set)
	}

	// wait for the previous scale-down to fully complete before
	// decrementing further, otherwise all pods terminate at once
	if set.Status.ObservedGeneration != set.Generation || set.Status.Replicas != ptr.Deref(set.Spec.Replicas, 1) {
		return false, nil
	}

	return false, retryUpdate(ctx, s.ctl, client.ObjectKeyFromObject(set), func(obj *appsv1.StatefulSet) {
		obj.Spec.Replicas = ptr.To(ptr.Deref(obj.Spec.Replicas, 1) - 1)
	})
}

func containsStatefulSet(sets []*appsv1.StatefulSet, target *appsv1.StatefulSet) bool {
	for _, set := range sets {
		if set.Name == target.Name && set.Namespace == target.Namespace {
			return true
		}
	}

	return false
}
