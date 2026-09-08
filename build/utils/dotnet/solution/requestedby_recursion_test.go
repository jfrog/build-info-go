package solution

import (
	"fmt"
	"testing"
	"time"

	buildinfo "github.com/jfrog/build-info-go/entities"
)

// TestPopulateRequestedByTerminatesOnDenseGraph guards the recursion brake in populateRequestedBy.
//
// The walk keeps no visited set: it terminates because RequestedBy grows until it trips the
// RequestedByMaxLength check. Anything that shrinks RequestedBy inside the loop - collapsing,
// deduplicating, truncating - removes that brake and turns the walk into a full root-to-node path
// enumeration. This graph (each package depending on the next three, the shape of an ordinary
// transitive NuGet closure) took over 10 seconds at 30 packages when a per-child dedupe was applied
// during the traversal, versus single-digit milliseconds without it. Deduplication belongs at emit
// time in BuildInfo.
func TestPopulateRequestedByTerminatesOnDenseGraph(t *testing.T) {
	const packages = 60
	dependenciesMap := make(map[string]*buildinfo.Dependency, packages)
	childrenMap := make(map[string][]string, packages)
	for i := 0; i < packages; i++ {
		id := fmt.Sprintf("pkg%03d", i)
		dependenciesMap[id] = &buildinfo.Dependency{Id: id}
		for step := 1; step <= 3; step++ {
			if i+step < packages {
				childrenMap[id] = append(childrenMap[id], fmt.Sprintf("pkg%03d", i+step))
			}
		}
	}
	root := dependenciesMap["pkg000"]
	root.RequestedBy = [][]string{{"module:1.0.0"}}

	finished := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		populateRequestedBy(*root, dependenciesMap, childrenMap)
		finished <- time.Since(start)
	}()

	select {
	case elapsed := <-finished:
		t.Logf("populateRequestedBy over %d packages: %s", packages, elapsed)
		if elapsed > 5*time.Second {
			t.Fatalf("populateRequestedBy took %s over %d packages; the recursion brake has been defeated", elapsed, packages)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("populateRequestedBy did not finish within 30s over %d packages; the recursion brake has been defeated", packages)
	}
}
