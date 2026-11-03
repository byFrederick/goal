package gitdocs

// SetMaxArchiveBytes lowers r's bound on an archive, so a test past it need not serve 200 MB.
func SetMaxArchiveBytes(r *Reader, n int64) { r.maxArchive = n }
