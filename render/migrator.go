package render

import (
	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	"github.com/redpanda-data/common-go/kube"
	"k8s.io/apimachinery/pkg/types"
)

const (
	oldToNewMigrationLabelStatus = "migration.lambda.coffee/new-to-old-migration/status"
	oldToNewMigrationLabelSource = "migration.lambda.coffee/new-to-old-migration/source"
)

type OldToNewMigrator struct {
	ctl *kube.Ctl
}

func (m *OldToNewMigrator) IsMigrating(old *migrationv1alpha1.Old) bool {
	labels := old.GetLabels()
	if labels == nil {
		return false
	}

	status, ok := labels[oldToNewMigrationLabelStatus]
	return ok && status == "migrating"
}

func (m *OldToNewMigrator) IsMigrated(old *migrationv1alpha1.Old) bool {
	if old.GetLabels() == nil {
		return false
	}

	status, ok := old.GetLabels()[oldToNewMigrationLabelStatus]
	return ok && status == "migrated"
}

func (m *OldToNewMigrator) MarkMigrated(old *migrationv1alpha1.Old) {
	labels := old.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[oldToNewMigrationLabelStatus] = "migrated"
	old.SetLabels(labels)
}

func (m *OldToNewMigrator) SourceFromTarget(new *migrationv1alpha1.New) *types.NamespacedName {
	labels := new.GetLabels()
	if labels == nil {
		return nil
	}

	source, ok := labels[oldToNewMigrationLabelSource]
	if !ok {
		return nil
	}

	return &types.NamespacedName{
		Name:      source,
		Namespace: new.Namespace,
	}
}

type OldToNewStatefulMigrator StatefulMigrator[migrationv1alpha1.Old, migrationv1alpha1.New, *migrationv1alpha1.Old, *migrationv1alpha1.New]

func NewOldToNewStatefulMigrator(ctl *kube.Ctl) OldToNewStatefulMigrator {
	return NewStatefulMigrator(ctl, &OldToNewMigrator{
		ctl: ctl,
	}, OldSyncerFactoryForKubeCtl(ctl), NewSyncerFactoryForKubeCtl(ctl))
}
