// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package routing

import (
	"strings"
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// eventWire draws a Filter and an Event out of the fuzzer's bytes. Each call
// consumes one byte and a read past the end returns zero, so a short input is
// a small filter rather than a panic — and the fuzzer's byte-level mutations
// turn into structural ones (a list appears, a type is swapped, the event
// changes body).
type eventWire struct {
	b []byte
	i int
}

func (w *eventWire) next() byte {
	if w.i >= len(w.b) {
		return 0
	}
	c := w.b[w.i]
	w.i++
	return c
}

func (w *eventWire) yes() bool { return w.next()%2 == 1 }

func (w *eventWire) pick(options []string) string {
	if len(options) == 0 {
		return ""
	}
	return options[int(w.next())%len(options)]
}

// The vocabularies are the spellings these fields actually carry: the methods
// the router serves, the globs the stream filter bar builds, the classes the
// UI's status chips send ("2".."5" — see useStreamFilters), and the model
// names of a registry. The fuzzed strings are mixed in beside them, so a
// mutation reaches both the well-formed shapes and anything else.
var (
	fuzzMethods = []string{"GET", "POST", "get", " PUT ", "DELETE", "PATCH", ""}
	fuzzGlobs   = []string{"/api/*", "/api", "/healthz", "*", "/*", "/api/v1/?", "", "/api/models/*/records"}
	fuzzClasses = []string{"2", "3", "4", "5", "503", "404", "0", "", " 5 "}
	fuzzModels  = []string{"AdminUser", "adminuser", " Note ", "Session", ""}
	fuzzTypes   = []adminv1.EventType{
		adminv1.EventType_EVENT_TYPE_UNSPECIFIED,
		adminv1.EventType_EVENT_TYPE_HTTP_REQUEST,
		adminv1.EventType_EVENT_TYPE_SQL_STATEMENT,
		adminv1.EventType_EVENT_TYPE_SESSION_CHANGE,
		adminv1.EventType_EVENT_TYPE_CUSTOM,
	}
)

// draw builds the Filter and the Event the target decides. The first two
// bytes of the input are a header, so a seed says plainly which clauses it is
// about: byte 0 is a bitmask of the lists the filter carries (an absent list
// means "match everything", which is a different decision from a list that
// matches nothing), byte 1 chooses the event's body. The bytes after them
// fill the lists in.
const (
	maskTypes = 1 << iota
	maskNodeIDs
	maskMethods
	maskGlobs
	maskClasses
	maskModels
)

func (w *eventWire) draw(nodeID, glob, class, reqPath string, status int32) (*adminv1.Filter, *adminv1.Event) {
	mask := w.next()
	body := w.next()

	f := &adminv1.Filter{}
	if mask&maskTypes != 0 {
		f.Types = append(f.Types, fuzzTypes[int(w.next())%len(fuzzTypes)])
		if w.yes() {
			f.Types = append(f.Types, fuzzTypes[int(w.next())%len(fuzzTypes)])
		}
	}
	if mask&maskNodeIDs != 0 {
		f.NodeIds = append(f.NodeIds, nodeID)
		if w.yes() {
			f.NodeIds = append(f.NodeIds, w.pick([]string{"node-a", "NODE-B", " node-c ", ""}))
		}
	}
	if mask&maskMethods != 0 {
		f.HttpMethods = append(f.HttpMethods, w.pick(fuzzMethods))
	}
	if mask&maskGlobs != 0 {
		f.HttpPathGlobs = append(f.HttpPathGlobs, glob)
		if w.yes() {
			f.HttpPathGlobs = append(f.HttpPathGlobs, w.pick(fuzzGlobs))
		}
	}
	if mask&maskClasses != 0 {
		f.HttpStatusClasses = append(f.HttpStatusClasses, class)
		if w.yes() {
			f.HttpStatusClasses = append(f.HttpStatusClasses, w.pick(fuzzClasses))
		}
	}
	if mask&maskModels != 0 {
		f.SqlModels = append(f.SqlModels, w.pick(fuzzModels))
	}

	// The event: one of the four bodies, or none — an event whose body this
	// server does not know, which a newer agent can send.
	e := &adminv1.Event{NodeId: nodeID}
	switch body % 5 {
	case 0, 1:
		e.Body = &adminv1.Event_HttpRequest{HttpRequest: &adminv1.HttpRequestEvent{
			Method: w.pick(fuzzMethods),
			Path:   reqPath,
			Status: uint32(status),
		}}
	case 2:
		e.Body = &adminv1.Event_SqlStatement{SqlStatement: &adminv1.SqlStatementEvent{
			ModelName: w.pick(fuzzModels),
			Operation: "select",
		}}
	case 3:
		e.Body = &adminv1.Event_SessionChange{SessionChange: &adminv1.SessionChangeEvent{}}
	case 4:
		e.Body = &adminv1.Event_Custom{Custom: &adminv1.CustomEvent{Name: w.pick(fuzzModels)}}
	}
	return f, e
}

// statusClassOracle is an independent reading of the contract the proto states
// for http_status_classes: "each entry is a digit string ('4', '5') that
// matches any status code in that hundred". It is written from that sentence,
// not from statusClassMatches: an entry is a run of ASCII digits, its first
// digit names the hundred, and a three-digit entry also names one exact
// status. An entry that is not a digit string names no status at all.
//
// This is the assertion that found the defect fixed alongside this target:
// statusClassMatches computed an "exact" status from any three-character
// entry by arithmetic on its bytes, so the class "0:0" — three characters,
// two of them digits — matched status 100 (0*100 + (':'-'0')*10 + 0).
func statusClassOracle(classes []string, status int) bool {
	if len(classes) == 0 {
		return true
	}
	for _, c := range classes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		digits := true
		for i := 0; i < len(c); i++ {
			if c[i] < '0' || c[i] > '9' {
				digits = false
				break
			}
		}
		if !digits {
			continue
		}
		if int(c[0]-'0') == status/100 {
			return true
		}
		if len(c) == 3 {
			exact := int(c[0]-'0')*100 + int(c[1]-'0')*10 + int(c[2]-'0')
			if exact == status {
				return true
			}
		}
	}
	return false
}

// FuzzEventFilterMatches drives the whole filter decision of the event stream,
// over both readers of it. A Filter arrives from the UI and an Event from an
// agent, so every string here is untrusted wire text.
//
// The properties:
//
//   - the live bus and the replay buffer decide the same event the same way.
//     replayMatches is, by its own comment, "a copy of EventSubscription.matches";
//     a copy is exactly the thing a differential is for, and the whole filter
//     is fuzzed because the parts that can drift are the ones a change touches.
//     F12 (maturity audit 2026-09-03) was this property failing on NodeIds:
//     exact on one side, folded on the other, so a filter matched a node's
//     live events and missed its replay, which reads to an operator as the
//     node having gone quiet.
//   - a match names one entry of each list the filter carries: the decision
//     decomposes, so no list can widen past its own contents.
//   - status classes obey the contract the proto writes down, read
//     independently by statusClassOracle rather than by calling the matcher
//     back.
//   - a "/*" glob is a subtree: the deliberate deviation from path.Match that
//     the hand-rolled dialect exists for. If it matches a path, it matches
//     everything under it.
//   - node-id matching is stable under padding on either side.
//   - an absent list is not a filter: an empty Filter passes every event.
func FuzzEventFilterMatches(f *testing.F) {
	seeds := []struct {
		shape   []byte
		nodeID  string
		glob    string
		class   string
		reqPath string
		status  int32
	}{
		// F12: a folded and padded node id, on a filter that carries only NodeIds.
		{[]byte{maskNodeIDs, 0}, " Node-A ", "/api/*", "5", "/api/models", 503},
		// The class that matched a status it does not name: three characters,
		// two of them digits, and status 100 was in nobody's filter.
		{[]byte{maskClasses, 0}, "node-a", "/api/*", "0:0", "/api/models", 100},
		// The same shape with a digit in the tens place and a class the UI sends.
		{[]byte{maskClasses, 0}, "node-a", "/api/*", "5a", "/api/models", 503},
		// Every list at once, over an HTTP event.
		{[]byte{maskTypes | maskNodeIDs | maskMethods | maskGlobs | maskClasses | maskModels, 0, 1, 0, 0, 0, 0, 0}, "node-b", "/api", "404", "/api/models/AdminUser", 404},
		// Globs against a path that is a subtree of them.
		{[]byte{maskGlobs, 1, 0, 1, 3}, "550e8400-e29b-41d4-a716-446655440000", "/api/v1/*", "2", "/api/v1/nodes/x/records", 200},
		// A SQL event against a model allow-list.
		{[]byte{maskModels | maskTypes, 2, 2, 0, 0}, "region/us-east/host", "/*", "503", "/", 503},
		// An event whose body this server does not know.
		{[]byte{maskTypes | maskGlobs, 4, 1, 0, 0}, "NODE-A\t", "/api/models/*/records", " 5 ", "/api/models/AdminUser/records", 500},
		{nil, "", "", "", "", 0},
	}
	for _, s := range seeds {
		f.Add(s.shape, s.nodeID, s.glob, s.class, s.reqPath, s.status)
	}

	f.Fuzz(func(t *testing.T, shape []byte, nodeID, glob, class, reqPath string, status int32) {
		w := &eventWire{b: shape}
		filter, event := w.draw(nodeID, glob, class, reqPath, status)

		sub := &EventSubscription{filter: filter}
		live := sub.matches(event)
		if replay := replayMatches(filter, event); replay != live {
			t.Fatalf("the live bus says %v and the replay buffer says %v for filter %v, event %v",
				live, replay, filter, event)
		}

		// The decision decomposes: if the whole filter matched, then each of
		// its lists, taken alone, matches too — and one entry of that list is
		// the one that did it. A list that widened past its contents would
		// break the second half.
		if live {
			for _, only := range oneListAtATime(filter) {
				if !replayMatches(only, event) {
					t.Fatalf("filter %v matched event %v, but its clause %v alone does not", filter, event, only)
				}
				if !anyEntryMatches(only, event) {
					t.Fatalf("clause %v matched event %v, but no single entry of it does: the list widened past its contents",
						only, event)
				}
			}
		}

		if http := event.GetHttpRequest(); http != nil {
			if got, want := statusClassMatches(filter.HttpStatusClasses, int(http.Status)), statusClassOracle(filter.HttpStatusClasses, int(http.Status)); got != want {
				t.Fatalf("status classes %q on status %d: the matcher says %v, the contract says %v",
					filter.HttpStatusClasses, http.Status, got, want)
			}
			// A "/*" glob names a subtree: whatever it matches, it matches
			// under too. This is the rule path.Match does not have and the
			// hand-rolled dialect was written for.
			//
			// The path under it is built from the trimmed path, because that
			// is the path pathMatches decides about — it trims the request
			// path before comparing. Appending to the raw one instead asks a
			// different question and the first mutation run found it: the
			// glob "/api/*" matches "/api " (which is "/api") and does not
			// match "/api /deeper" (which is not under it). The property was
			// wrong there, not the matcher; the input is kept as a seed.
			under := strings.TrimRight(strings.TrimSpace(http.Path), "/") + "/deeper"
			for _, g := range filter.HttpPathGlobs {
				if !strings.HasSuffix(strings.TrimSpace(g), "/*") {
					continue
				}
				if pathMatches([]string{g}, http.Path) && !pathMatches([]string{g}, under) {
					t.Fatalf("glob %q matched %q but not %q, the path under it", g, http.Path, under)
				}
			}
		}

		// Padding is not identity: a filter typed with a stray space, or an id
		// that reached the wire padded, still names the same node.
		if len(filter.NodeIds) > 0 {
			padded := make([]string, len(filter.NodeIds))
			for i, id := range filter.NodeIds {
				padded[i] = " " + id + "\t"
			}
			want := nodeIDMatches(filter.NodeIds, event.NodeId)
			if got := nodeIDMatches(padded, "\n"+event.NodeId+" "); got != want {
				t.Fatalf("padding changed the match for ids %q, node %q: %v vs %v",
					filter.NodeIds, event.NodeId, got, want)
			}
		}

		// An absent list is not a filter: an empty Filter passes every event,
		// on both readers, so a filter that names nothing never silences a node.
		empty := &adminv1.Filter{}
		if !replayMatches(empty, event) || !(&EventSubscription{filter: empty}).matches(event) {
			t.Fatalf("an empty filter rejected event %v", event)
		}
	})
}

// oneListAtATime returns one Filter per populated list of f, each carrying
// that list alone.
func oneListAtATime(f *adminv1.Filter) []*adminv1.Filter {
	var out []*adminv1.Filter
	if len(f.Types) > 0 {
		out = append(out, &adminv1.Filter{Types: f.Types})
	}
	if len(f.NodeIds) > 0 {
		out = append(out, &adminv1.Filter{NodeIds: f.NodeIds})
	}
	if len(f.HttpMethods) > 0 {
		out = append(out, &adminv1.Filter{HttpMethods: f.HttpMethods})
	}
	if len(f.HttpPathGlobs) > 0 {
		out = append(out, &adminv1.Filter{HttpPathGlobs: f.HttpPathGlobs})
	}
	if len(f.HttpStatusClasses) > 0 {
		out = append(out, &adminv1.Filter{HttpStatusClasses: f.HttpStatusClasses})
	}
	if len(f.SqlModels) > 0 {
		out = append(out, &adminv1.Filter{SqlModels: f.SqlModels})
	}
	return out
}

// anyEntryMatches reports whether some single entry of the one list f carries
// decides the event on its own.
func anyEntryMatches(f *adminv1.Filter, e *adminv1.Event) bool {
	var singles []*adminv1.Filter
	for _, t := range f.Types {
		singles = append(singles, &adminv1.Filter{Types: []adminv1.EventType{t}})
	}
	for _, id := range f.NodeIds {
		singles = append(singles, &adminv1.Filter{NodeIds: []string{id}})
	}
	for _, m := range f.HttpMethods {
		singles = append(singles, &adminv1.Filter{HttpMethods: []string{m}})
	}
	for _, g := range f.HttpPathGlobs {
		singles = append(singles, &adminv1.Filter{HttpPathGlobs: []string{g}})
	}
	for _, c := range f.HttpStatusClasses {
		singles = append(singles, &adminv1.Filter{HttpStatusClasses: []string{c}})
	}
	for _, m := range f.SqlModels {
		singles = append(singles, &adminv1.Filter{SqlModels: []string{m}})
	}
	for _, s := range singles {
		if replayMatches(s, e) {
			return true
		}
	}
	return false
}
