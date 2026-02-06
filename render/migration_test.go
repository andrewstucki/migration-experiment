package render

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// --- Test CRD definitions ---

const (
	testPackage         = "example.com"
	testOld             = "MyKindOld"
	testNew             = "MyKindNew"
	testOldNameSingular = "mykindold"
	testOldNamePlural   = "mykindolds"
	testNewNameSingular = "mykindnew"
	testNewNamePlural   = "mykindnews"
	testOldCRDName      = testOldNamePlural + "." + testPackage
	testNewCRDName      = testNewNamePlural + "." + testPackage

	version = "v1alpha1"
)

var (
	testOldGK = schema.GroupKind{
		Group: testPackage,
		Kind:  testOld,
	}
	testNewGK = schema.GroupKind{
		Group: testPackage,
		Kind:  testNew,
	}

	testOldGVK = testOldGK.WithVersion(version)
	testNewGVK = testNewGK.WithVersion(version)
)

type myKindOld struct {
	metav1.TypeMeta   `json:",inline"`            //nolint:revive // test code
	metav1.ObjectMeta `json:"metadata,omitempty"` //nolint:revive // test code
}

func (m *myKindOld) DeepCopyObject() runtime.Object {
	return &myKindOld{
		TypeMeta:   m.TypeMeta,
		ObjectMeta: *m.ObjectMeta.DeepCopy(),
	}
}

type myKindNew struct {
	metav1.TypeMeta   `json:",inline"`            //nolint:revive // test code
	metav1.ObjectMeta `json:"metadata,omitempty"` //nolint:revive // test code
}

func (m *myKindNew) DeepCopyObject() runtime.Object {
	return &myKindNew{
		TypeMeta:   m.TypeMeta,
		ObjectMeta: *m.ObjectMeta.DeepCopy(),
	}
}

func setupCRDs(t *testing.T, c *kube.Ctl) {
	t.Helper()

	setupCRD(t, c, testOldCRDName, apiextensionsv1.CustomResourceDefinitionNames{
		Singular: testOldNameSingular,
		Plural:   testOldNamePlural,
		Kind:     testOld,
	})

	setupCRD(t, c, testNewCRDName, apiextensionsv1.CustomResourceDefinitionNames{
		Singular: testNewNameSingular,
		Plural:   testNewNamePlural,
		Kind:     testNew,
	})
}

func setupCRD(t *testing.T, c *kube.Ctl, name string, names apiextensionsv1.CustomResourceDefinitionNames) {
	t.Helper()

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: testPackage,
			Names: names,
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:       version,
				Deprecated: false,
				Served:     true,
				Storage:    true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:       "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{},
					},
				},
			}},
		},
	}

	if err := c.Create(t.Context(), crd); err != nil {
		t.Fatalf("create crd: %v", err)
	}

	if err := wait.PollUntilContextTimeout(t.Context(), 500*time.Millisecond, 15*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.Get(ctx, types.NamespacedName{Name: name}, crd); err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("crd not established: %v", err)
	}
}

const (
	testSourceLabel       = "test/migration-source"
	testStatusLabel       = "test/migration-status"
	testOldNameLabel      = "test/old-name"
	testOldNamespaceLabel = "test/old-namespace"
	testNewNameLabel      = "test/new-name"
	testNewNamespaceLabel = "test/new-namespace"
)

type testMigrator struct{}

func (m *testMigrator) SourceFromTarget(target *myKindNew) *types.NamespacedName {
	labels := target.GetLabels()
	if labels == nil {
		return nil
	}
	source, ok := labels[testSourceLabel]
	if !ok {
		return nil
	}
	return &types.NamespacedName{Name: source, Namespace: target.Namespace}
}

func (m *testMigrator) MarkMigrated(source *myKindOld) {
	labels := source.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[testStatusLabel] = "migrated"
	source.SetLabels(labels)
}

func (m *testMigrator) IsMigrated(source *myKindOld) bool {
	labels := source.GetLabels()
	return labels != nil && labels[testStatusLabel] == "migrated"
}

func (m *testMigrator) IsMigrating(source *myKindOld) bool {
	labels := source.GetLabels()
	return labels != nil && labels[testStatusLabel] == "migrating"
}

type testSyncer struct {
	labels map[string]string
	syncFn func(context.Context) ([]client.Object, error)
	listFn func(context.Context) ([]client.Object, error)
}

func (s *testSyncer) DeleteAll(context.Context) (bool, error) { return false, nil }

func (s *testSyncer) Sync(ctx context.Context) ([]client.Object, error) {
	if s.syncFn != nil {
		return s.syncFn(ctx)
	}
	return nil, nil
}

func (s *testSyncer) ListInPurview(ctx context.Context) ([]client.Object, error) {
	if s.listFn != nil {
		return s.listFn(ctx)
	}
	return nil, nil
}

func (s *testSyncer) OwnerLabels() map[string]string {
	return s.labels
}

type testSyncerFactory[T any, PT PObject[T]] struct {
	ownerLabelsFn func(PT) map[string]string
	syncFn        func(context.Context) ([]client.Object, error)
	listFn        func(context.Context) ([]client.Object, error)
}

func (f *testSyncerFactory[T, PT]) Syncer(state PT) Syncer {
	return &testSyncer{
		labels: f.ownerLabelsFn(state),
		syncFn: f.syncFn,
		listFn: f.listFn,
	}
}

func oldOwnerLabels(o *myKindOld) map[string]string {
	return map[string]string{
		testOldNameLabel:      o.GetName(),
		testOldNamespaceLabel: o.GetNamespace(),
	}
}

func newOwnerLabels(n *myKindNew) map[string]string {
	return map[string]string{
		testNewNameLabel:      n.GetName(),
		testNewNamespaceLabel: n.GetNamespace(),
	}
}

func TestStatefulMigrator(t *testing.T) {
	createNamespace := func(t *testing.T, ctl *kube.Ctl) string {
		t.Helper()
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
			},
		}
		if err := ctl.Create(t.Context(), ns); err != nil {
			t.Fatalf("creating namespace: %v", err)
		}
		return ns.Name
	}

	makeOld := func(name, namespace string, labels map[string]string) *myKindOld {
		return &myKindOld{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
		}
	}

	makeNew := func(name, namespace string, labels map[string]string) *myKindNew {
		return &myKindNew{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
		}
	}

	makeStatefulSet := func(name, namespace string, labels map[string]string, replicas int32) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "test-svc",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				Replicas: ptr.To(replicas),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "test"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "test",
							Image: "busybox:latest",
						}},
					},
				},
			},
		}
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding apiextensions scheme: %v", err)
	}

	testGV := schema.GroupVersion{Group: testPackage, Version: version}
	scheme.AddKnownTypeWithName(testOldGVK, &myKindOld{})
	scheme.AddKnownTypeWithName(testNewGVK, &myKindNew{})
	metav1.AddToGroupVersion(scheme, testGV)

	ctl := kubetest.NewEnv(t, kube.Options{
		Options: client.Options{
			Scheme: scheme,
		},
	})

	setupCRDs(t, ctl)

	sm := NewStatefulMigrator(ctl, &testMigrator{},
		&testSyncerFactory[myKindOld, *myKindOld]{
			ownerLabelsFn: func(o *myKindOld) map[string]string {
				return oldOwnerLabels(o)
			},
		},
		&testSyncerFactory[myKindNew, *myKindNew]{
			ownerLabelsFn: func(n *myKindNew) map[string]string {
				return newOwnerLabels(n)
			},
		},
	)

	t.Run("ShouldMigrateSource", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name   string
			labels map[string]string
			want   bool
		}{
			{"no labels", nil, false},
			{"unrelated labels", map[string]string{"foo": "bar"}, false},
			{"migrating", map[string]string{testStatusLabel: "migrating"}, true},
			{"migrated", map[string]string{testStatusLabel: "migrated"}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				old := &myKindOld{ObjectMeta: metav1.ObjectMeta{Labels: tc.labels}}
				if got := sm.ShouldMigrateSource(old); got != tc.want {
					t.Errorf("ShouldMigrateSource() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("ShouldMigrateTarget", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name   string
			labels map[string]string
			want   bool
		}{
			{"no labels", nil, false},
			{"unrelated labels", map[string]string{"foo": "bar"}, false},
			{"has source label", map[string]string{testSourceLabel: "some-old"}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				n := &myKindNew{ObjectMeta: metav1.ObjectMeta{Labels: tc.labels}}
				if got := sm.ShouldMigrateTarget(n); got != tc.want {
					t.Errorf("ShouldMigrateTarget() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("EnsureMigrated", func(t *testing.T) {
		t.Parallel()

		t.Run("no migration target", func(t *testing.T) {
			t.Parallel()

			ns := createNamespace(t, ctl)

			target := makeNew("target", ns, nil)
			if err := ctl.Create(t.Context(), target); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(t.Context(), target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source != nil {
				t.Fatal("expected nil source")
			}
		})

		t.Run("source not found", func(t *testing.T) {
			t.Parallel()

			ns := createNamespace(t, ctl)

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "nonexistent",
			})
			if err := ctl.Create(t.Context(), target); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(t.Context(), target)
			if !errors.Is(err, ErrMigrationSourceNotFound) {
				t.Fatalf("expected ErrMigrationSourceNotFound, got: %v", err)
			}
			if source != nil {
				t.Fatal("expected nil source")
			}
		})

		t.Run("unmatched migration labels", func(t *testing.T) {
			t.Parallel()

			ns := createNamespace(t, ctl)

			old := makeOld("source", ns, nil)
			if err := ctl.Create(t.Context(), old); err != nil {
				t.Fatal(err)
			}

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "source",
			})
			if err := ctl.Create(t.Context(), target); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(t.Context(), target)
			if !errors.Is(err, ErrUnmatchedMigrationTarget) {
				t.Fatalf("expected ErrUnmatchedMigrationTarget, got: %v", err)
			}
			if source != nil {
				t.Fatal("expected nil source")
			}
		})

		t.Run("already migrated", func(t *testing.T) {
			t.Parallel()

			ns := createNamespace(t, ctl)

			old := makeOld("source", ns, map[string]string{
				testStatusLabel: "migrated",
			})
			if err := ctl.Create(t.Context(), old); err != nil {
				t.Fatal(err)
			}

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "source",
			})
			if err := ctl.Create(t.Context(), target); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(t.Context(), target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source == nil {
				t.Fatal("expected non-nil source")
			}
			if source.Name != "source" {
				t.Errorf("expected source name 'source', got %q", source.Name)
			}
		})

		t.Run("completes migration", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			ns := createNamespace(t, ctl)

			old := makeOld("source", ns, map[string]string{
				testStatusLabel: "migrating",
			})
			if err := ctl.Create(ctx, old); err != nil {
				t.Fatal(err)
			}

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "source",
			})
			if err := ctl.Create(ctx, target); err != nil {
				t.Fatal(err)
			}

			set := makeStatefulSet("test-set", ns, oldOwnerLabels(old), 3)
			if err := controllerutil.SetControllerReference(old, set, ctl.Scheme()); err != nil {
				t.Fatal(err)
			}
			if err := ctl.Create(ctx, set); err != nil {
				t.Fatal(err)
			}

			// Mark StatefulSet as fully rolled out so checkMigrated succeeds
			set.Status = appsv1.StatefulSetStatus{
				ObservedGeneration: set.Generation,
				Replicas:           3,
				UpdatedReplicas:    3,
			}
			if err := ctl.UpdateStatus(ctx, set); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(ctx, target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source == nil {
				t.Fatal("expected non-nil source")
			}

			// Old should now be marked as migrated
			updatedOld := &myKindOld{}
			if err := ctl.Get(ctx, client.ObjectKeyFromObject(old), updatedOld); err != nil {
				t.Fatal(err)
			}
			if updatedOld.Labels[testStatusLabel] != "migrated" {
				t.Errorf("expected Old status label 'migrated', got labels: %v", updatedOld.Labels)
			}

			// StatefulSet ownership should be transferred to New
			var updatedSet appsv1.StatefulSet
			if err := ctl.Get(ctx, client.ObjectKeyFromObject(set), &updatedSet); err != nil {
				t.Fatal(err)
			}

			if updatedSet.Labels[testNewNameLabel] != target.Name {
				t.Errorf("expected New name label %q, got %q", target.Name, updatedSet.Labels[testNewNameLabel])
			}
			if updatedSet.Labels[testNewNamespaceLabel] != target.Namespace {
				t.Errorf("expected New namespace label %q, got %q", target.Namespace, updatedSet.Labels[testNewNamespaceLabel])
			}
			if _, ok := updatedSet.Labels[testOldNameLabel]; ok {
				t.Error("expected Old name label to be removed from StatefulSet")
			}
			if _, ok := updatedSet.Labels[testOldNamespaceLabel]; ok {
				t.Error("expected Old namespace label to be removed from StatefulSet")
			}

			var hasNewRef, hasOldRef bool
			for _, ref := range updatedSet.OwnerReferences {
				if ref.UID == target.UID {
					hasNewRef = true
				}
				if ref.UID == old.UID {
					hasOldRef = true
				}
			}
			if !hasNewRef {
				t.Error("expected StatefulSet to have New as controller owner")
			}
			if hasOldRef {
				t.Error("expected Old OwnerRef to be removed from StatefulSet")
			}
		})

		t.Run("migration in progress", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			ns := createNamespace(t, ctl)

			old := makeOld("source", ns, map[string]string{
				testStatusLabel: "migrating",
			})
			if err := ctl.Create(ctx, old); err != nil {
				t.Fatal(err)
			}

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "source",
			})
			if err := ctl.Create(ctx, target); err != nil {
				t.Fatal(err)
			}

			// StatefulSet owned by Old — status left at zero values so
			// ObservedGeneration(0) != Generation(1) → migration incomplete
			set := makeStatefulSet("test-set", ns, oldOwnerLabels(old), 3)
			if err := controllerutil.SetControllerReference(old, set, ctl.Scheme()); err != nil {
				t.Fatal(err)
			}
			if err := ctl.Create(ctx, set); err != nil {
				t.Fatal(err)
			}

			source, err := sm.EnsureMigrated(ctx, target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source == nil {
				t.Fatal("expected non-nil source")
			}

			// Old should NOT be marked as migrated yet
			updatedOld := &myKindOld{}
			if err := ctl.Get(ctx, client.ObjectKeyFromObject(old), updatedOld); err != nil {
				t.Fatal(err)
			}
			if updatedOld.Labels[testStatusLabel] != "migrating" {
				t.Errorf("expected Old to still have 'migrating' label, got labels: %v", updatedOld.Labels)
			}

			// StatefulSet should still be adopted even though migration not complete
			var updatedSet appsv1.StatefulSet
			if err := ctl.Get(ctx, client.ObjectKeyFromObject(set), &updatedSet); err != nil {
				t.Fatal(err)
			}
			if updatedSet.Labels[testNewNameLabel] != target.Name {
				t.Errorf("expected StatefulSet to have New name label after adoption, got labels: %v", updatedSet.Labels)
			}
		})

		t.Run("multiple statefulsets", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			ns := createNamespace(t, ctl)

			old := makeOld("source", ns, map[string]string{
				testStatusLabel: "migrating",
			})
			if err := ctl.Create(ctx, old); err != nil {
				t.Fatal(err)
			}

			target := makeNew("target", ns, map[string]string{
				testSourceLabel: "source",
			})
			if err := ctl.Create(ctx, target); err != nil {
				t.Fatal(err)
			}

			for _, name := range []string{"set-a", "set-b"} {
				set := makeStatefulSet(name, ns, oldOwnerLabels(old), 3)
				if err := controllerutil.SetControllerReference(old, set, ctl.Scheme()); err != nil {
					t.Fatal(err)
				}
				if err := ctl.Create(ctx, set); err != nil {
					t.Fatal(err)
				}
				set.Status = appsv1.StatefulSetStatus{
					ObservedGeneration: set.Generation,
					Replicas:           3,
					UpdatedReplicas:    3,
				}
				if err := ctl.UpdateStatus(ctx, set); err != nil {
					t.Fatal(err)
				}
			}

			source, err := sm.EnsureMigrated(ctx, target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if source == nil {
				t.Fatal("expected non-nil source")
			}

			for _, name := range []string{"set-a", "set-b"} {
				var set appsv1.StatefulSet
				if err := ctl.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &set); err != nil {
					t.Fatalf("getting %s: %v", name, err)
				}
				if set.Labels[testNewNameLabel] != target.Name {
					t.Errorf("%s: expected New name label, got labels: %v", name, set.Labels)
				}
				if _, ok := set.Labels[testOldNameLabel]; ok {
					t.Errorf("%s: expected Old name label removed", name)
				}
			}

			updatedOld := &myKindOld{}
			if err := ctl.Get(ctx, client.ObjectKeyFromObject(old), updatedOld); err != nil {
				t.Fatal(err)
			}
			if updatedOld.Labels[testStatusLabel] != "migrated" {
				t.Errorf("expected Old status 'migrated', got labels: %v", updatedOld.Labels)
			}
		})
	})
}
