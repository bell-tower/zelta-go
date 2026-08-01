// Package zconf parses zelta.conf policy documents (sites, hosts, options,
// datasets, import: splicing) into ordered backup jobs. The grammar is
// deliberately line-based, not YAML (tabs rejected, 2-space indentation,
// import: fragments). String import surface: zconf.Load. Orchestration
// (running jobs, filtering, formatting) stays in internal/policy.
package zconf
