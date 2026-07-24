package projection

// FaultPoint identifies a deterministic rebuild boundary available to tests
// and controlled diagnostic callers. Hooks never alter source authority.
type FaultPoint string

const (
	FaultAfterSchema            FaultPoint = "after_schema"
	FaultAfterDataWrite         FaultPoint = "after_data_write"
	FaultBeforeIntegrityCheck   FaultPoint = "before_integrity_check"
	FaultAfterIntegrityCheck    FaultPoint = "after_integrity_check"
	FaultAfterConnectionClose   FaultPoint = "after_connection_close"
	FaultAfterSourceRecheck     FaultPoint = "after_source_recheck"
	FaultAfterVisibleActivation FaultPoint = "after_visible_activation"
	FaultBeforeParentSync       FaultPoint = "before_parent_sync"
)

// IntegrityPurpose records why the exact external-content consistency SQL ran.
type IntegrityPurpose string

const (
	IntegrityBuildCompletion IntegrityPurpose = "build_completion"
	IntegrityFreshActivation IntegrityPurpose = "fresh_activation"
	IntegrityCorruptionCheck IntegrityPurpose = "corruption_check"
)

// Hooks supplies deterministic fault and integrity evidence seams. Production
// callers normally leave both callbacks nil.
type Hooks struct {
	OnFault                 func(FaultPoint) error
	OnIntegrity             func(IntegrityPurpose)
	OnBeforeReaderOpen      func() error
	OnAfterNamespaceReplace func() error
	OnQuarantineRename      func(source, destination string) error
}

func (hooks Hooks) afterNamespaceReplace() error {
	if hooks.OnAfterNamespaceReplace == nil {
		return nil
	}
	return hooks.OnAfterNamespaceReplace()
}

func (hooks Hooks) beforeQuarantineRename(source, destination string) error {
	if hooks.OnQuarantineRename == nil {
		return nil
	}
	return hooks.OnQuarantineRename(source, destination)
}

func (hooks Hooks) beforeReaderOpen() error {
	if hooks.OnBeforeReaderOpen == nil {
		return nil
	}
	return hooks.OnBeforeReaderOpen()
}

func (hooks Hooks) fault(point FaultPoint) error {
	if hooks.OnFault == nil {
		return nil
	}
	return hooks.OnFault(point)
}

func (hooks Hooks) integrity(purpose IntegrityPurpose) {
	if hooks.OnIntegrity != nil {
		hooks.OnIntegrity(purpose)
	}
}
