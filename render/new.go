package render

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
)

func RenderNew(ctx context.Context, state *migrationv1alpha1.New) ([]kube.Object, error) {
	return []kube.Object{}, nil
}

func NewRenderedTypes() []kube.Object {
	// remove job, secret, configmap, and all non-core types other than certmanager stuff
	return []kube.Object{
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&policyv1.PodDisruptionBudget{},
		&rbacv1.ClusterRoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.RoleBinding{},
		&rbacv1.Role{},
		// additional non-core types
		&certmanagerv1.Certificate{},
		&certmanagerv1.Issuer{},
	}
}

func NewStatefulSets(image migrationv1alpha1.Image, state *migrationv1alpha1.New) []*appsv1.StatefulSet {
	return []*appsv1.StatefulSet{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.GetName(),
			Namespace: state.GetNamespace(),
			Labels:    NewOwnershipLabels(state),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				// this also needs to be identical to the old selector
				MatchLabels: statefulsetSelectorLabels("set"),
			},
			Replicas: state.Spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: mergeLabels(NewOwnershipLabels(state), statefulsetSelectorLabels("set")),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "new-container",
							Image:   fmt.Sprintf("%s:%s", image.Repository, image.Tag),
							Command: []string{"/migration-operator", "entry"},
							VolumeMounts: []corev1.VolumeMount{{
								Name:      "new-data",
								MountPath: "/data",
							}},
						},
					},
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "new-data",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ki"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Ki"),
						},
					},
				},
			}},
		},
	}}
}

func NewOwnershipLabels(state *migrationv1alpha1.New) map[string]string {
	return map[string]string{
		NewNameLabelKey:      state.GetName(),
		NewNamespaceLabelKey: state.GetNamespace(),
	}
}
