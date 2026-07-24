//go:build windows

package workspace

func platformDurabilityClaim() string {
	return "file_content_synced_directory_metadata_best_effort_on_windows"
}
