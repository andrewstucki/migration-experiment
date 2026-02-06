package render

import (
	"github.com/redpanda-data/common-go/kube"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	migrationkube "github.com/andrewstucki/migration-experiment/kube"
)

const (
	oldToNewMigrationLabelStatus    = "migration.lambda.coffee/new-to-old-migration-status"
	oldToNewMigrationLabelSource    = "migration.lambda.coffee/new-to-old-migration-source"
	oldToNewMigrationLabelTarget    = "migration.lambda.coffee/new-to-old-migration-target"
	oldToNewMigrationLabelMigrating = "migration.lambda.coffee/new-to-old-migration-migrating"
)

var OldToNewStatefulMigrationLabels = migrationkube.MigrationLabels[migrationv1alpha1.Old, migrationv1alpha1.New, *migrationv1alpha1.Old, *migrationv1alpha1.New]{
	StatusLabel:    oldToNewMigrationLabelStatus,
	SourceLabel:    oldToNewMigrationLabelSource,
	TargetLabel:    oldToNewMigrationLabelTarget,
	MigratingLabel: oldToNewMigrationLabelMigrating,
}

type OldToNewStatefulMigrator = migrationkube.StatefulMigrator[migrationv1alpha1.Old, migrationv1alpha1.New, *migrationv1alpha1.Old, *migrationv1alpha1.New]

func NewOldToNewStatefulMigrator(ctl *kube.Ctl) OldToNewStatefulMigrator {
	return migrationkube.NewStatefulMigrator(ctl, OldToNewStatefulMigrationLabels, OldSyncerFactoryForKubeCtl(ctl), NewSyncerFactoryForKubeCtl(ctl))
}
