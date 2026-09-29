package gitsync

// RecordedMergeOutput exposes the recorded merge output to external tests.
func RecordedMergeOutput(r *Repo) string { return r.recordedMergeOutput() }
