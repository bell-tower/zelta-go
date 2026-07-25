// Package opt loads option defaults and the Zelta env hierarchy.
//
// Hierarchy (partial): built-in defaults → process env ZELTA_* / KEY.
// zelta.env files and full opts.tsv CLI parsing come later (conf + CLI).
package opt
