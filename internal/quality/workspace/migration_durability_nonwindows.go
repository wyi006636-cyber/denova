//go:build !windows

package workspace

func platformDurabilityClaim() string {
	return "file_and_supported_directory_metadata_synced_before_state_advance"
}
