package pipeline

import (
	"slices"
	"sort"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// Clusters maps each local namespace to the Go package that carries it.
//
// A namespace usually gets its own package. Mutually recursive namespaces cannot:
// Go's package is its unit of mutual recursion, and an import cycle is rejected
// outright. So every strongly-connected component of the namespace reference graph is
// emitted as ONE package.
//
// That is not a workaround, it is the correct mapping. C#'s unit of mutual recursion
// is the assembly, and Microsoft ships all fourteen mutually recursive
// Microsoft.UI.Xaml.* namespaces in a single assembly. A Go package is the direct
// analogue; splitting them was the error.
//
// The alternative, which this replaces, was to sever the cheapest edge in each cycle
// and degrade every reference across it. Measured against this metadata that cost the
// whole of UIElement's input surface — every pointer, keyboard and manipulation event
// — because the args types live in Microsoft.UI.Xaml.Input while the events are
// declared in Microsoft.UI.Xaml. One cut, several hundred lost members.
//
// Collapsing components also makes the package graph acyclic by construction, so
// after this there is nothing left to sever.
type Clusters struct {
	// packageOf maps a namespace to its cluster's representative namespace, which
	// names the emitted package. A namespace in no cluster maps to itself.
	packageOf map[string]string
	// members maps a representative to every namespace it carries, sorted.
	members map[string][]string
}

// ComputeClusters finds the strongly-connected components of the local namespace
// reference graph and returns the namespace-to-package mapping.
//
// External namespaces are excluded: nothing in go-bindings-winrt imports this module,
// so no reference into it can be part of a cycle here.
func ComputeClusters(registry *Registry) *Clusters {
	edges := localReferenceGraph(registry)

	clusters := &Clusters{
		packageOf: make(map[string]string, len(registry.Namespaces)),
		members:   map[string][]string{},
	}
	for _, component := range stronglyConnected(edges, registry) {
		// The representative names the package. The shortest name wins, ties broken
		// lexicographically: for the XAML cluster that is Microsoft.UI.Xaml, the
		// common root of all fourteen, which is also the name a reader would expect.
		sort.Slice(component, func(i, j int) bool {
			if len(component[i]) != len(component[j]) {
				return len(component[i]) < len(component[j])
			}
			return component[i] < component[j]
		})
		representative := component[0]
		sorted := slices.Clone(component)
		sort.Strings(sorted)
		clusters.members[representative] = sorted
		for _, namespace := range component {
			clusters.packageOf[namespace] = representative
		}
	}
	return clusters
}

// localReferenceGraph is the namespace-to-namespace reference graph over local
// namespaces only, unweighted — component membership does not care how many times an
// edge is used, only whether it exists.
func localReferenceGraph(registry *Registry) map[string]map[string]bool {
	edges := make(map[string]map[string]bool, len(registry.Namespaces))
	for _, meta := range registry.Namespaces {
		targets := map[string]bool{}
		wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
			if ref.Kind == "Native" || ref.Namespace == "" || ref.Namespace == meta.Namespace {
				return
			}
			if registry.IsLocal(ref.Namespace) {
				targets[ref.Namespace] = true
			}
		})
		// Inherited-interface edges too: the class emitter projects each base class's
		// interfaces onto the derived class, so the derived namespace references them
		// even though its own TypeRefs never mention them. Missing these is what
		// produced a package cycle the breaker had not accounted for.
		for name := range meta.Classes {
			class := meta.Classes[name]
			registry.WalkBaseChain(&class, func(_ string, base *wasdkmeta.Class) {
				for i := range base.Interfaces {
					target := base.Interfaces[i].Namespace
					if target != "" && target != meta.Namespace && registry.IsLocal(target) {
						targets[target] = true
					}
				}
			})
		}
		edges[meta.Namespace] = targets
	}
	return edges
}

// stronglyConnected returns Tarjan's strongly-connected components. Iteration order
// is sorted throughout, so the same graph always yields the same components in the
// same order — the committed tree depends on it.
func stronglyConnected(edges map[string]map[string]bool, registry *Registry) [][]string {
	index := map[string]int{}
	lowlink := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var components [][]string
	counter := 0

	nodes := make([]string, 0, len(registry.Namespaces))
	for _, meta := range registry.Namespaces {
		nodes = append(nodes, meta.Namespace)
	}
	sort.Strings(nodes)

	sortedTargets := func(node string) []string {
		targets := make([]string, 0, len(edges[node]))
		for target := range edges[node] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		return targets
	}

	// Iterative Tarjan. The recursive form is clearer, but the XAML cluster is
	// fourteen namespaces deep in a graph of seventy-seven and a hostile metadata
	// update should fail rather than blow the stack.
	type frame struct {
		node    string
		targets []string
		next    int
	}
	var visit func(root string)
	visit = func(root string) {
		frames := []frame{{node: root, targets: sortedTargets(root)}}
		index[root], lowlink[root] = counter, counter
		counter++
		stack = append(stack, root)
		onStack[root] = true

		for len(frames) > 0 {
			top := &frames[len(frames)-1]
			if top.next < len(top.targets) {
				target := top.targets[top.next]
				top.next++
				if _, seen := index[target]; !seen {
					index[target], lowlink[target] = counter, counter
					counter++
					stack = append(stack, target)
					onStack[target] = true
					frames = append(frames, frame{node: target, targets: sortedTargets(target)})
				} else if onStack[target] {
					if index[target] < lowlink[top.node] {
						lowlink[top.node] = index[target]
					}
				}
				continue
			}
			// Every edge explored: close the node.
			node := top.node
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := frames[len(frames)-1].node
				if lowlink[node] < lowlink[parent] {
					lowlink[parent] = lowlink[node]
				}
			}
			if lowlink[node] == index[node] {
				var component []string
				for {
					member := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[member] = false
					component = append(component, member)
					if member == node {
						break
					}
				}
				components = append(components, component)
			}
		}
	}

	for _, node := range nodes {
		if _, seen := index[node]; !seen {
			visit(node)
		}
	}
	return components
}

// PackageOf returns the namespace whose name the emitted package takes. For a
// namespace in no cluster that is the namespace itself.
func (c *Clusters) PackageOf(namespace string) string {
	if representative, ok := c.packageOf[namespace]; ok {
		return representative
	}
	return namespace
}

// SamePackage reports whether two namespaces are emitted into one Go package, which
// is what makes a reference between them package-local rather than an import.
func (c *Clusters) SamePackage(a, b string) bool {
	return c.PackageOf(a) == c.PackageOf(b)
}

// Members returns the namespaces a package carries, sorted. A single-namespace
// package returns just itself.
func (c *Clusters) Members(representative string) []string {
	if members, ok := c.members[representative]; ok {
		return members
	}
	return []string{representative}
}

// Merged returns every representative whose package carries more than one namespace,
// sorted — the clusters worth reporting.
func (c *Clusters) Merged() []string {
	var merged []string
	for representative, members := range c.members {
		if len(members) > 1 {
			merged = append(merged, representative)
		}
	}
	sort.Strings(merged)
	return merged
}
