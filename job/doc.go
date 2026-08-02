// Package job is the public import/export document for Zelta Action objects.
//
// A Document is a versioned list of Items. Each Item names an op (backup,
// match, …) and carries a typed Request and optional Result. JSON is the
// built-in encoding; types are Marshal-friendly for other formats.
//
//	doc, err := job.Decode(r)
//	for _, it := range doc.Items {
//	    switch it.Op {
//	    case job.OpBackup:
//	        res, err := backup.Run(ctx, exec, *it.Backup)
//	    case job.OpMatch:
//	        res, err := match.Compare(ctx, exec, *it.Match)
//	    }
//	}
//
// Native SDK JSON is the public wire format. The Awk-oracle CLI schema
// (zelta backup --json) stays in internal/report.
//
// Process-bound fields (OnLine callbacks, Executor, DatasetContext) are never
// serialized. zconf remains a separate conf dialect; it is expected to become
// an internal loader that feeds the same Request types this package marshals.
package job
