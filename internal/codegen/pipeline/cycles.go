package pipeline

import (
	"slices"
	"sort"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// defaultEmbedWeight makes a default-interface edge expensive to sever, because
// severing one demotes a whole runtime class: the generated class struct embeds its
// default interface, so losing that import costs every member of the class rather
// than one signature.
const defaultEmbedWeight = 1000

// inheritedInterfaceWeight is what one inherited-interface edge is worth: the same
// as one plain reference, counted once per interface reachable over it.
//
// Weighting them higher was tried, on the reasoning that losing one removes a whole
// As<Interface> accessor rather than one signature, and it made the output worse:
// raising these weights only moves the cut onto other edges in the same cycles, and
// those carry more plain references. Across the tree it took the degradation count
// from 870 to 1040 without recovering the accessor it was aimed at
// (Controls → Controls.Primitives, which Button needs for Click).
//
// The reason no weighting recovers that particular edge is that Controls ↔
// Controls.Primitives is genuinely mutual and dense in both directions. One of them
// has to go, and Go offers no way to express the cycle. What is NOT lost is the
// capability: the runtime's QueryInterface is generic, so a caller whose own package
// imports both can still reach the interface directly —
//
//	base, err := winrt.QueryInterface[primitives.IButtonBase](
//	    unsafe.Pointer(button), &primitives.IID_IButtonBase)
//
// — because a consuming package is not part of the generated tree and closes no
// cycle. Only the convenience accessor is absent, on about twenty edges out of the
// whole graph.
const inheritedInterfaceWeight = 1

// ComputeBlockedImports builds the cross-PACKAGE reference graph, detects import
// cycles, and returns the edge set to sever: blocked[src][dst] means references from
// package src to package dst must degrade to a raw type instead of importing.
//
// Since mutually recursive namespaces are merged into single packages (see
// ComputeClusters), the package graph is acyclic by construction and this returns an
// empty set. It is kept as a backstop rather than deleted: a future metadata shape
// that reintroduced a package cycle would otherwise surface as a compile failure
// across the whole tree instead of as diagnostics naming the members lost.
//
// Only LOCAL namespaces participate. An import into go-bindings-winrt can never close
// a cycle, because nothing in that module imports this one.
func ComputeBlockedImports(registry *Registry, clusters *Clusters) map[string]map[string]bool {
	packageOf := func(namespace string) string {
		if clusters == nil {
			return namespace
		}
		return clusters.PackageOf(namespace)
	}
	edges := map[string]map[string]int{}
	for _, meta := range registry.Namespaces {
		source := packageOf(meta.Namespace)
		weights := edges[source]
		if weights == nil {
			weights = map[string]int{}
		}
		count := func(ref *wasdkmeta.TypeRef) {
			if ref.Kind == "Native" || ref.Namespace == "" {
				return
			}
			if !registry.IsLocal(ref.Namespace) {
				return
			}
			if target := packageOf(ref.Namespace); target != source {
				weights[target]++
			}
		}
		wasdkmeta.WalkRefs(meta, count)
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.DefaultInterface != nil {
				namespace := class.DefaultInterface.Namespace
				if namespace != "" && registry.IsLocal(namespace) {
					if target := packageOf(namespace); target != source {
						weights[target] += defaultEmbedWeight
					}
				}
			}
			// Inherited-interface edges. The class emitter walks the Extends chain
			// and projects each base class's interfaces as query methods ON THIS
			// class, so the derived class's package imports them — an edge that
			// exists nowhere in this namespace's own TypeRefs. Counting it here is
			// what keeps this graph the same graph the emitter builds; without it,
			// severing decides against edges it cannot see and the output does not
			// compile.
			registry.WalkBaseChain(&class, func(_ string, base *wasdkmeta.Class) {
				for i := range base.Interfaces {
					namespace := base.Interfaces[i].Namespace
					if namespace != "" && registry.IsLocal(namespace) {
						if target := packageOf(namespace); target != source {
							weights[target] += inheritedInterfaceWeight
						}
					}
				}
			})
		}
		if len(weights) > 0 {
			edges[source] = weights
		}
	}

	blocked := map[string]map[string]bool{}
	for {
		cycle := findCycle(edges)
		if cycle == nil {
			return blocked
		}
		src, dst := lightestEdge(edges, cycle)
		delete(edges[src], dst)
		if blocked[src] == nil {
			blocked[src] = map[string]bool{}
		}
		blocked[src][dst] = true
	}
}

// findCycle DFSes the edge graph and returns one cycle as a node path
// (v0 → v1 → … → v0), or nil when the graph is acyclic. Iteration order is
// sorted, so the same graph always yields the same cycle.
func findCycle(edges map[string]map[string]int) []string {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var stack []string
	var cycle []string

	nodes := make([]string, 0, len(edges))
	for node := range edges {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	var visit func(node string) bool
	visit = func(node string) bool {
		state[node] = inStack
		stack = append(stack, node)
		targets := make([]string, 0, len(edges[node]))
		for target := range edges[node] {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			switch state[target] {
			case inStack:
				// Slice the cycle out of the stack.
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == target {
						cycle = append(slices.Clone(stack[i:]), target)
						return true
					}
				}
			case unvisited:
				if visit(target) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = done
		return false
	}
	for _, node := range nodes {
		if state[node] == unvisited && visit(node) {
			return cycle
		}
	}
	return nil
}

// lightestEdge picks the cycle edge with the fewest references (cheapest to
// degrade), breaking ties by name for determinism.
func lightestEdge(edges map[string]map[string]int, cycle []string) (string, string) {
	bestSrc, bestDst := cycle[0], cycle[1]
	bestWeight := edges[bestSrc][bestDst]
	for i := range len(cycle) - 1 {
		src, dst := cycle[i], cycle[i+1]
		weight := edges[src][dst]
		if weight < bestWeight || (weight == bestWeight && src+"→"+dst < bestSrc+"→"+bestDst) {
			bestSrc, bestDst, bestWeight = src, dst, weight
		}
	}
	return bestSrc, bestDst
}
