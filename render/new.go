package render

import (
	"context"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
)

func RenderNew(ctx context.Context, state *migrationv1alpha1.New) ([]kube.Object, error) {
	return []kube.Object{}, nil
}

func NewRenderedTypes() []kube.Object {
	// remove job, secret, configmap, and all non-core types other than certmanager stuff
	return []kube.Object{
		&appsv1.StatefulSet{},
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

func NewOwnershipLabels(state *migrationv1alpha1.New) map[string]string {
	return map[string]string{
		NewNameLabelKey:      state.GetName(),
		NewNamespaceLabelKey: state.GetNamespace(),
	}
}
