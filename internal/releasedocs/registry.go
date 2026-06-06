package releasedocs

// registry.go declares the registry contract for the release-docs subsystem.
// The two built-in generators (changelog, releasenotes) and two built-in
// publishers (releasebody, changelogpr) are wired in
// internal/releasedocs/defaults — a thin, cycle-free wire package that imports
// both this package and the generator/publisher sub-packages.
//
// The generators and publishers import this package (releasedocs) for their
// core types (Generator, Publisher, Artifact, ReleaseContext, …), which means
// registry wiring CANNOT live inside this package without creating an import
// cycle. Placing the wiring in internal/releasedocs/defaults breaks the cycle:
//
//	defaults → releasedocs (types)
//	defaults → releasedocs/generators/changelog (implements Generator)
//	defaults → releasedocs/publishers/releasebody  (implements Publisher)
//
// Cmd binaries and the CLI dispatcher obtain the default slices from defaults:
//
//	import "github.com/payamqorbanpour/cadoo/internal/releasedocs/defaults"
//	d.Generators = defaults.DefaultGenerators()
//	d.Publishers = defaults.DefaultPublishers()
//
// Tests construct their own slices of fakes and never import defaults.
