package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"reflect"
	"sort"
)

const topologyHashPrefix = "sha256:"

type topologyEdge struct {
	source string
	target string
}

type topologyVisitState uint8

const (
	topologyUnseen topologyVisitState = iota
	topologyVisiting
	topologyVisited
)

type topologyScan struct {
	edges map[string][]string
	state map[string]topologyVisitState
	back  map[topologyEdge]struct{}
}

func (wf *Workflow[I, O]) compiledTopologyVersion(plugins pluginSetup) string {
	digest := sha256.New()
	writeTopologyFact(digest, "workflow", wf.name, wf.topologyVersion, wf.entry, wf.maxSteps, wf.maxParallelism)
	if wf.contextSet {
		writeTopologyFact(digest, "context", typeIdentity(wf.contextType))
	}
	for _, name := range sortedStringKeys(wf.nodes) {
		writeTopologyFact(digest, "node", name)
		for _, fact := range wf.nodes[name].topologyFacts() {
			writeTopologyFact(digest, "node-fact", fact)
		}
	}
	for _, source := range sortedStringKeys(wf.edges) {
		targets := append([]string(nil), wf.edges[source]...)
		sort.Strings(targets)
		for _, target := range targets {
			writeTopologyFact(digest, "edge", source, target)
		}
	}
	for _, exit := range sortedStringKeys(wf.exits) {
		writeTopologyFact(digest, "exit", exit)
	}
	writePluginTopology(digest, wf.plugins, plugins)
	for _, hook := range wf.beforeWorkflow {
		writeTopologyFact(digest, "before-workflow", hook.Name)
	}
	for _, hook := range wf.afterWorkflow {
		writeTopologyFact(digest, "after-workflow", hook.Name)
	}
	return topologyHashPrefix + hex.EncodeToString(digest.Sum(nil))
}

func (n *Node[I, O]) topologyFacts() []string {
	facts := []string{
		typeIdentity(n.inputType()), typeIdentity(n.outputType()),
		fmt.Sprint(n.run != nil), fmt.Sprint(n.join != nil), fmt.Sprint(n.route != nil),
		fmt.Sprint(n.merge), fmt.Sprint(n.invokable),
	}
	for _, guard := range n.guards {
		facts = append(facts, "guard:"+string(guard.phase)+":"+guard.name)
	}
	for _, hook := range n.before {
		facts = append(facts, "before:"+hook.Name)
	}
	for _, hook := range n.after {
		facts = append(facts, "after:"+hook.Name)
	}
	return facts
}

func writePluginTopology(digest hash.Hash, configured []Plugin, plugins pluginSetup) {
	for _, plugin := range configured {
		writeTopologyFact(digest, "plugin", plugin.Name())
	}
	for _, middleware := range plugins.nodeMiddlewares {
		writeTopologyFact(digest, "node-middleware", middleware.name)
	}
	for _, middleware := range plugins.routeMiddlewares {
		writeTopologyFact(digest, "route-middleware", middleware.name)
	}
	for _, middleware := range plugins.joinMiddlewares {
		writeTopologyFact(digest, "join-middleware", middleware.name)
	}
	for _, eventType := range sortedStringKeys(plugins.eventTypes) {
		writeTopologyFact(digest, "event-type", eventType)
	}
	for _, name := range plugins.topologyNames {
		writeTopologyFact(digest, "registry", name)
	}
}

func writeTopologyFact(digest hash.Hash, values ...any) {
	for _, value := range values {
		_, _ = fmt.Fprintf(digest, "%T:%v\x00", value, value)
	}
	_, _ = digest.Write([]byte{'\n'})
}

func typeIdentity(value reflect.Type) string {
	if value == nil {
		return "<nil>"
	}
	return value.PkgPath() + ":" + value.String()
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func findTopologyBackEdges(entry string, edges map[string][]string) map[topologyEdge]struct{} {
	scan := &topologyScan{edges: edges, state: map[string]topologyVisitState{}, back: map[topologyEdge]struct{}{}}
	scan.visit(entry)
	return scan.back
}

func (scan *topologyScan) visit(node string) {
	scan.state[node] = topologyVisiting
	targets := append([]string(nil), scan.edges[node]...)
	sort.Strings(targets)
	for _, target := range targets {
		scan.visitEdge(node, target)
	}
	scan.state[node] = topologyVisited
}

func (scan *topologyScan) visitEdge(source, target string) {
	if scan.state[target] == topologyVisiting {
		scan.back[topologyEdge{source: source, target: target}] = struct{}{}
		return
	}
	if scan.state[target] == topologyUnseen {
		scan.visit(target)
	}
}

func (c *compiled[I, O]) nextCorrelation(source activation, target string) CorrelationKey {
	correlation := source.correlation
	if _, back := c.backEdges[topologyEdge{source: source.node, target: target}]; back {
		correlation.Epoch++
	}
	return correlation
}
