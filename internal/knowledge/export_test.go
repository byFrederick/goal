package knowledge

// SetByteLimits lowers in's limits on a source's Markdown and each file's, so a test past them
// need not compress 50 MB.
func (in *Ingestor) SetByteLimits(source, file int64) {
	in.limits = byteLimits{source: source, file: file}
}
