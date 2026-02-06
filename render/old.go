package render

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
)

func RenderOld(ctx context.Context, state *migrationv1alpha1.Old) ([]kube.Object, error) {
	return []kube.Object{}, nil
}

func OldRenderedTypes() []kube.Object {
	return []kube.Object{
		&batchv1.Job{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&policyv1.PodDisruptionBudget{},
		&rbacv1.ClusterRoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.RoleBinding{},
		&rbacv1.Role{},
		// additional non-core types
		&autoscalingv2.HorizontalPodAutoscaler{},
		&certmanagerv1.Certificate{},
		&certmanagerv1.Issuer{},
		&monitoringv1.PodMonitor{},
		&monitoringv1.ServiceMonitor{},
		&networkingv1.Ingress{},
	}
}

func OldStatefulSet(image migrationv1alpha1.Image, state *migrationv1alpha1.Old) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: OldOwnershipLabels(state),
			},
			Replicas: state.Spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: OldOwnershipLabels(state),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "old-container",
							Image:   fmt.Sprintf("%s:%s", image.Repository, image.Tag),
							Command: []string{"/migration-operator", "entry"},
							VolumeMounts: []corev1.VolumeMount{{
								Name:      "old-data",
								MountPath: "/data",
							}},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "old-data",
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
	}
}

func OldOwnershipLabels(state *migrationv1alpha1.Old) map[string]string {
	return map[string]string{
		OldNameLabelKey:      state.GetName(),
		OldNamespaceLabelKey: state.GetNamespace(),
	}
}
