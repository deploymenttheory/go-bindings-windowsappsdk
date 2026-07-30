package pipeline

import (
	"slices"
	"sort"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// defaultEmbedWeight makes a default-interface edge expensive to sever, because
// severing one demotes a whole runtime class: the generated class struct embeds
// its default interface, so losing that import costs every member of the class
// rather than one signature.
const defaultEmbedWeight = 1000

// ComputeBlockedImports builds the cross-namespace reference graph, detects
// import cycles, and returns the edge set to sever: blocked[src][dst] means
// references from src to dst must degrade to a raw type instead of importing.
//
// Go rejects an import cycle outright, and WinRT namespaces reference each other
// freely in both directions — Microsoft.UI.Xaml and Microsoft.UI.Xaml.Controls
// each name types in the other. Something has to give, so the cheapest edge in
// each cycle is cut: fewest references degraded, ties broken by name so the
// result is the same on every run.
//
// Only LOCAL namespaces participate. An import into go-bindings-winrt can never
// close a cycle, because nothing in that module imports this one.
func ComputeBlockedImports(registry *Registry) map[string]map[string]bool {
	edges := map[string]map[string]int{}
	for _, meta := range registry.Namespaces {
		weights := map[string]int{}
		count := func(ref *wasdkmeta.TypeRef) {
			if ref.Kind == "Native" || ref.Namespace == "" || ref.Namespace == meta.Namespace {
				return
			}
			if !registry.IsLocal(ref.Namespace) {
				return
			}
			weights[ref.Namespace]++
		}
		wasdkmeta.WalkRefs(meta, count)
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.DefaultInterface == nil {
				continue
			}
			target := class.DefaultInterface.Namespace
			if target == "" || target == meta.Namespace || !registry.IsLocal(target) {
				continue
			}
			weights[target] += defaultEmbedWeight
		}
		if len(weights) > 0 {
			edges[meta.Namespace] = weights
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
