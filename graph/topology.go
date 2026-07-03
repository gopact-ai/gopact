package graph

import "sort"

// Topology describes the static structure of a graph for audits, diagrams, and tooling.
//
// Branch functions are opaque at inspection time, so Branches records the branch
// source and branch-function count instead of possible runtime targets.
type Topology struct {
	Nodes    []TopologyNode   `json:"nodes"`
	Edges    []TopologyEdge   `json:"edges"`
	Branches []TopologyBranch `json:"branches,omitempty"`
	Joins    []TopologyJoin   `json:"joins,omitempty"`
	MaxSteps int              `json:"max_steps,omitempty"`
}

// TopologyNodeKind classifies a graph topology node.
type TopologyNodeKind string

const (
	// TopologyNodeBoundary marks the virtual Start or End node.
	TopologyNodeBoundary TopologyNodeKind = "boundary"
	// TopologyNodeFunction marks a registered NodeFunc.
	TopologyNodeFunction TopologyNodeKind = "function"
	// TopologyNodeRunnable marks a graph node backed by another Runnable.
	TopologyNodeRunnable TopologyNodeKind = "runnable"
)

// TopologyNode describes one graph node in a topology export.
type TopologyNode struct {
	Name string           `json:"name"`
	Kind TopologyNodeKind `json:"kind"`
}

// TopologyEdge describes one static graph edge.
//
// Index preserves the edge's insertion order among edges with the same source.
type TopologyEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Index int    `json:"index"`
}

// TopologyBranch describes opaque dynamic branch functions attached to a node.
type TopologyBranch struct {
	From  string `json:"from"`
	Count int    `json:"count"`
}

// TopologyJoin describes a node that waits for multiple predecessor nodes.
type TopologyJoin struct {
	Node         string   `json:"node"`
	Predecessors []string `json:"predecessors"`
}

// Topology returns a stable, defensive topology export for this graph.
func (g *Graph[S]) Topology() Topology {
	if g == nil {
		return Topology{}
	}
	return buildTopology(topologyNodeKinds(g.nodes, g.runnableNodes), g.edges, g.branches, joinPredecessors(g.edges), 0)
}

// Topology returns a stable, defensive topology export for this compiled runnable.
func (r *Runnable[S]) Topology() Topology {
	if r == nil {
		return Topology{}
	}
	return buildTopology(r.nodeKinds, r.edges, r.branches, r.joins, r.maxSteps)
}

func buildTopology[S any](
	nodeKinds map[string]TopologyNodeKind,
	edges map[string][]string,
	branches map[string][]BranchFunc[S],
	joins map[string][]string,
	maxSteps int,
) Topology {
	return Topology{
		Nodes:    topologyNodes(nodeKinds),
		Edges:    topologyEdges(edges),
		Branches: topologyBranches(branches),
		Joins:    topologyJoins(joins),
		MaxSteps: maxSteps,
	}
}

func topologyNodeKinds[S any](
	nodes map[string]NodeFunc[S],
	runnableNodes map[string]*Runnable[S],
) map[string]TopologyNodeKind {
	nodeKinds := make(map[string]TopologyNodeKind, len(nodes))
	for name := range nodes {
		kind := TopologyNodeFunction
		if _, ok := runnableNodes[name]; ok {
			kind = TopologyNodeRunnable
		}
		nodeKinds[name] = kind
	}
	return nodeKinds
}

func topologyNodes(nodeKinds map[string]TopologyNodeKind) []TopologyNode {
	nodes := make([]TopologyNode, 0, len(nodeKinds)+2)
	nodes = append(nodes, TopologyNode{Name: Start, Kind: TopologyNodeBoundary})
	names := make([]string, 0, len(nodeKinds))
	for name := range nodeKinds {
		if name == Start || name == End {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		kind := nodeKinds[name]
		if kind == "" {
			kind = TopologyNodeFunction
		}
		nodes = append(nodes, TopologyNode{Name: name, Kind: kind})
	}
	nodes = append(nodes, TopologyNode{Name: End, Kind: TopologyNodeBoundary})
	return nodes
}

func topologyEdges(edges map[string][]string) []TopologyEdge {
	out := make([]TopologyEdge, 0)
	froms := make([]string, 0, len(edges))
	for from := range edges {
		froms = append(froms, from)
	}
	sortTopologyNames(froms)
	for _, from := range froms {
		for i, to := range edges[from] {
			out = append(out, TopologyEdge{From: from, To: to, Index: i})
		}
	}
	return out
}

func topologyBranches[S any](branches map[string][]BranchFunc[S]) []TopologyBranch {
	out := make([]TopologyBranch, 0, len(branches))
	froms := make([]string, 0, len(branches))
	for from := range branches {
		froms = append(froms, from)
	}
	sortTopologyNames(froms)
	for _, from := range froms {
		out = append(out, TopologyBranch{From: from, Count: len(branches[from])})
	}
	return out
}

func topologyJoins(joins map[string][]string) []TopologyJoin {
	out := make([]TopologyJoin, 0, len(joins))
	nodes := make([]string, 0, len(joins))
	for node := range joins {
		nodes = append(nodes, node)
	}
	sortTopologyNames(nodes)
	for _, node := range nodes {
		out = append(out, TopologyJoin{
			Node:         node,
			Predecessors: append([]string(nil), joins[node]...),
		})
	}
	return out
}

func sortTopologyNames(names []string) {
	sort.Slice(names, func(i, j int) bool {
		leftRank, left := topologyNameSortKey(names[i])
		rightRank, right := topologyNameSortKey(names[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return left < right
	})
}

func topologyNameSortKey(name string) (int, string) {
	switch name {
	case Start:
		return 0, ""
	case End:
		return 2, ""
	default:
		return 1, name
	}
}
